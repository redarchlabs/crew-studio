package tracker

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
)

// People of a KM2 organisation, to name assignees, leads and comment authors. The whole person list is read once
// (up to km2PeopleCacheMax people) and kept per connection; a person missing from it makes the list be read again,
// at most once every km2PeopleRefresh. An organisation with more people than that is read person by person instead
// (id:in: chunks of 200), keeping what was found.

// km2PeopleCacheMax is the most people read in one go for the cache (a var so tests can lower it)
var km2PeopleCacheMax = 5_000

const (
	km2PeopleRefresh = 5 * time.Minute
	// maxKM2NameChars caps a person's name as Studio uses it (mention lines, comment authors)
	maxKM2NameChars = 120
)

// km2PeopleCache is one connection's people.
type km2PeopleCache struct {
	mu     sync.Mutex
	people map[string]Person
	// loaded is when the whole list was last read (or tried)
	loaded time.Time
	// byID is set once the organisation proved to have more people than the cache reads in one go
	byID bool
	now  func() time.Time
}

// km2Caches holds the people of every connection a Service makes clients for, so watches and runs do not read the
// whole list on every call.
type km2Caches struct {
	mu     sync.Mutex
	byConn map[string]*km2PeopleCache
	now    func() time.Time
}

func newKM2Caches(now func() time.Time) *km2Caches {
	if now == nil {
		now = time.Now
	}
	return &km2Caches{byConn: map[string]*km2PeopleCache{}, now: now}
}

// get is the cache of a connection; key "" (a client made outside a Service) gets one of its own.
func (c *km2Caches) get(key string) *km2PeopleCache {
	if c == nil || key == "" {
		return &km2PeopleCache{people: map[string]Person{}, now: time.Now}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if p, ok := c.byConn[key]; ok {
		return p
	}
	p := &km2PeopleCache{people: map[string]Person{}, now: c.now}
	c.byConn[key] = p
	return p
}

// cleanName makes a name from KM2 safe to write into a comment or a message: no control characters or line breaks,
// single spaces, at most maxKM2NameChars characters.
func cleanName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > maxKM2NameChars {
		s = string(r[:maxKM2NameChars])
	}
	return s
}

func personOf(r record) Person {
	return Person{AccountID: r.str("id"), Name: cleanName(r.str("name")), Email: oneLine(r.str("email"), 320)}
}

// peopleByID names the people with these record ids; ids KM2 does not know are left out.
func (k *km2Tracker) peopleByID(ctx context.Context, ids []string) (map[string]Person, error) {
	c := k.cache
	c.mu.Lock()
	defer c.mu.Unlock()
	missing := func() []string {
		var out []string
		for _, id := range ids {
			if _, ok := c.people[id]; !ok && id != "" && !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
		return out
	}
	miss := missing()
	if len(miss) > 0 && !c.byID && (c.loaded.IsZero() || c.now().Sub(c.loaded) >= km2PeopleRefresh) {
		c.loaded = c.now()
		recs, err := k.list(ctx, "person", nil, "created_at", "asc", km2PeopleCacheMax)
		var le *LimitError
		switch {
		case errors.As(err, &le):
			c.byID = true
		case err != nil:
			return nil, err
		default:
			c.people = make(map[string]Person, len(recs))
			for _, r := range recs {
				c.people[r.str("id")] = personOf(r)
			}
		}
		miss = missing()
	}
	if len(miss) > 0 && c.byID {
		for chunk := range slices.Chunk(miss, km2InLimit) {
			recs, err := k.list(ctx, "person", []string{filter("id", "in", strings.Join(chunk, ","))}, "created_at", "asc", 0)
			if err != nil {
				return nil, err
			}
			for _, r := range recs {
				c.people[r.str("id")] = personOf(r)
			}
		}
	}
	out := make(map[string]Person, len(ids))
	for _, id := range ids {
		if p, ok := c.people[id]; ok {
			out[id] = p
		}
	}
	return out, nil
}

// person names a person by record id; one KM2 does not know is known by its id only.
func (k *km2Tracker) person(ctx context.Context, id string) (*Person, error) {
	if id == "" {
		return nil, nil
	}
	people, err := k.peopleByID(ctx, []string{id})
	if err != nil {
		return nil, err
	}
	if p, ok := people[id]; ok {
		return &p, nil
	}
	return &Person{AccountID: id}, nil
}

// knowPeople reads the people a batch of records points at in one go, so naming them later reads nothing.
func (k *km2Tracker) knowPeople(ctx context.Context, recs []record, field string) error {
	ids := make([]string, 0, len(recs))
	for _, r := range recs {
		if id := r.str(field); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	_, err := k.peopleByID(ctx, ids)
	return err
}
