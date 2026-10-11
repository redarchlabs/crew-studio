package tracker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"crewstudio/internal/connections"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

const (
	testTimeout = 15 * time.Second
	// myselfTTL is how long a connection's own account is remembered (to leave its own comments out)
	myselfTTL = 10 * time.Minute
	// MaxPeopleSync bounds one people-map refresh
	MaxPeopleSync = 2000
)

// Service builds tracker clients for connections, opening their secrets on the server. Secrets never leave it.
type Service struct {
	store *store.Store
	conns *connections.Service
	opts  Options

	mu     sync.Mutex
	myself map[string]cachedPerson
	// calls throttles run-driven calls per connection, so one crew cannot use up a credential others share
	calls map[string]*rate.Limiter
}

const (
	// Per connection, for calls agents make during runs (watch checks are bounded separately)
	runCallsPerSecond = 5
	runCallBurst      = 30
)

// Allow reports whether a run may make another tracker call through the connection now.
func (s *Service) Allow(connectionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.calls[connectionID]
	if !ok {
		l = rate.NewLimiter(runCallsPerSecond, runCallBurst)
		s.calls[connectionID] = l
	}
	return l.Allow()
}

// Forget drops what the service remembers about a connection (its own account), after its settings or secret change.
func (s *Service) Forget(connectionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.myself, connectionID)
}

type cachedPerson struct {
	person Person
	at     time.Time
}

// NewService builds the service with the server's tracker options.
func NewService(st *store.Store, conns *connections.Service, opts Options) *Service {
	if opts.km2 == nil {
		opts.km2 = newKM2Caches(nil)
	}
	return &Service{store: st, conns: conns, opts: opts, myself: map[string]cachedPerson{}, calls: map[string]*rate.Limiter{}}
}

// Client opens a connection's secrets and builds its client.
func (s *Service) Client(ctx context.Context, conn model.Connection) (Client, error) {
	if !model.IsTrackerKind(conn.Kind) {
		return nil, errorf("%s is a %s connection, not a tracker (Jira, Linear or KM2 issues) one", conn.Name, conn.Kind)
	}
	secrets, err := s.conns.Secrets(ctx, conn.ID)
	if err != nil {
		return nil, errorf("the secrets of %s cannot be read: %v", conn.Name, err)
	}
	return New(conn, secrets, s.opts)
}

// WithSecrets builds a connection's client from secrets the caller already opened.
func (s *Service) WithSecrets(conn model.Connection, secrets map[string]string) (Client, error) {
	return New(conn, secrets, s.opts)
}

// UseProblem says why a crew cannot use a connection as a tracker, or "".
func UseProblem(use store.CrewConnectionUse) string {
	c := use.Connection
	switch {
	case !model.IsTrackerKind(c.Kind):
		return fmt.Sprintf("the connection %s is a %s connection, not a tracker (Jira, Linear or KM2 issues) one", c.Name, c.Kind)
	case !c.Enabled:
		return fmt.Sprintf("the connection %s is disabled", c.Name)
	case !use.Linked:
		return fmt.Sprintf("the crew may not use the connection %s (choose it on the crew's State tab)", c.Name)
	case !use.SharesArea:
		return fmt.Sprintf("the connection %s shares no functional area with the crew", c.Name)
	}
	return ""
}

// ForAgentChat loads a tracker connection an agent-chat turn froze into its spec and builds its client. Such a turn has
// no crew, so there is no crew link or crew area to check: the run-state API has already checked that the turn froze
// the connection and that the person chatting can still see it. The connection must still be enabled and of the
// right kind.
func (s *Service) ForAgentChat(ctx context.Context, connectionID string) (model.Connection, Client, error) {
	c, err := s.store.GetConnection(ctx, connectionID)
	if errors.Is(err, store.ErrNotFound) {
		return model.Connection{}, nil, errorf("the connection no longer exists")
	}
	if err != nil {
		return model.Connection{}, nil, err
	}
	if problem := UseProblem(store.CrewConnectionUse{Connection: c, Linked: true, SharesArea: true}); problem != "" {
		return c, nil, errorf("%s", problem)
	}
	client, err := s.Client(ctx, c)
	return c, client, err
}

// ForCrew loads a connection a crew may use as a tracker and builds its client.
func (s *Service) ForCrew(ctx context.Context, connectionID, crewID string) (model.Connection, Client, error) {
	use, err := s.store.ConnectionForCrew(ctx, connectionID, crewID)
	if errors.Is(err, store.ErrNotFound) {
		return model.Connection{}, nil, errorf("the connection no longer exists")
	}
	if err != nil {
		return model.Connection{}, nil, err
	}
	if problem := UseProblem(use); problem != "" {
		return use.Connection, nil, errorf("%s", problem)
	}
	client, err := s.Client(ctx, use.Connection)
	return use.Connection, client, err
}

// Myself returns the connection's own account, remembered for a few minutes.
func (s *Service) Myself(ctx context.Context, connectionID string, c Client) (Person, error) {
	s.mu.Lock()
	cached, ok := s.myself[connectionID]
	s.mu.Unlock()
	if ok && time.Since(cached.at) < myselfTTL {
		return cached.person, nil
	}
	p, err := c.Myself(ctx)
	if err != nil {
		return Person{}, err
	}
	s.mu.Lock()
	s.myself[connectionID] = cachedPerson{person: p, at: time.Now()}
	s.mu.Unlock()
	return p, nil
}

// Test checks a tracker connection by asking whose account its credentials are, and, for a KM2 organisation, that
// it has the tracker entities.
func (s *Service) Test(ctx context.Context, conn model.Connection) connections.TestResult {
	res := connections.TestResult{Supported: true}
	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()
	start := time.Now()
	client, err := s.Client(ctx, conn)
	var me Person
	if err == nil {
		me, err = client.Myself(ctx)
	}
	res.DurationMS = time.Since(start).Milliseconds()
	if err != nil {
		var te *Error
		if errors.As(err, &te) {
			res.StatusCode = te.Status
		}
		res.Message = "Could not use the connection: " + safeMessage(err)
		return res
	}
	checker, ok := client.(Checker)
	if !ok {
		res.OK = true
		res.Message = fmt.Sprintf("Connected as %s.", me.Name)
		return res
	}
	found, err := checker.Check(ctx)
	res.DurationMS = time.Since(start).Milliseconds()
	switch {
	case err != nil:
		res.Message = fmt.Sprintf("Connected as %s, but the tracker entities could not be checked: %s", me.Name, safeMessage(err))
	case len(found.Problems) > 0:
		res.Message = fmt.Sprintf("Connected as %s, but the KM2 organisation is not ready: %s.", me.Name, strings.Join(found.Problems, "; "))
	default:
		res.OK = true
		res.Message = fmt.Sprintf("Connected as %s. The organisation has the tracker entities.", me.Name)
	}
	if len(found.Warnings) > 0 {
		res.Message += " Warning: " + strings.Join(found.Warnings, "; ") + "."
	}
	return res
}

// SyncPeople reads the tracker's people into the connection's people map and links them to Studio users by email.
// It returns how many accounts were read and how many are linked.
func (s *Service) SyncPeople(ctx context.Context, conn model.Connection) (int, int, error) {
	client, err := s.Client(ctx, conn)
	if err != nil {
		return 0, 0, err
	}
	people, err := client.Users(ctx, MaxPeopleSync)
	if err != nil {
		return 0, 0, err
	}
	rows := make([]model.TrackerPerson, 0, len(people))
	for _, p := range people {
		if model.ValidAccountID(p.AccountID) {
			rows = append(rows, model.TrackerPerson{AccountID: p.AccountID, DisplayName: oneLine(p.Name, 200), Email: oneLine(p.Email, 320)})
		}
	}
	linked, err := s.store.SeeTrackerPeople(ctx, conn.ID, rows, time.Now())
	return len(rows), linked, err
}

// SafeMessage is what people and agents are told about a tracker error: tracker and limit errors as they are,
// anything else in general terms.
func SafeMessage(err error) string { return safeMessage(err) }

func safeMessage(err error) string {
	var te *Error
	var le *LimitError
	switch {
	case errors.As(err, &te):
		return te.Message
	case errors.As(err, &le):
		return le.Error()
	case errors.Is(err, context.DeadlineExceeded):
		return "the tracker did not answer in time"
	}
	return "the request failed on the server; see the service log"
}

// maxCountedChildren bounds the child count shown when adding an item.
const maxCountedChildren = 500

// ConnRef names a connection without its settings or secrets.
type ConnRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// ItemCheck is what adding an item to a crew's tracking list would track.
type ItemCheck struct {
	Connection ConnRef `json:"connection"`
	Issue      Issue   `json:"issue"`
	// DirectChildren counts the item's direct children (up to 500; Capped says there are more)
	DirectChildren int  `json:"direct_children"`
	Capped         bool `json:"children_capped"`
}

// CheckItem confirms a crew may use the connection as a tracker and that the item exists and can be seen.
func (s *Service) CheckItem(ctx context.Context, crewID, connectionID, ref string) (ItemCheck, error) {
	conn, client, err := s.ForCrew(ctx, connectionID, crewID)
	if err != nil {
		return ItemCheck{}, err
	}
	issue, err := client.Issue(ctx, ref)
	if IsNotFound(err) {
		return ItemCheck{}, &Error{NotFound: true, Message: fmt.Sprintf("%s was not found in %s, or the connection cannot see it", ref, conn.Name)}
	}
	if err != nil {
		return ItemCheck{}, err
	}
	out := ItemCheck{Connection: ConnRef{ID: conn.ID, Name: conn.Name, Kind: conn.Kind}, Issue: issue}
	children, err := client.Search(ctx, Query{Parent: &issue, IncludeDone: true, Max: maxCountedChildren})
	var limit *LimitError
	switch {
	case errors.As(err, &limit):
		out.DirectChildren, out.Capped = maxCountedChildren, true
	case err != nil:
		return ItemCheck{}, err
	default:
		out.DirectChildren = len(children)
	}
	return out, nil
}

// Options are the service's tracker options.
func (s *Service) Options() Options { return s.opts }
