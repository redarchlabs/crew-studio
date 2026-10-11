package watch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"crewstudio/internal/model"
)

const maxSourceBytes = 10 << 20

// Request is everything a source needs for one fetch. Def is already normalized (model.NormalizeWatch).
type Request struct {
	Def model.WatchInput
	// Connection and Secrets are set when the source kind reads through a connection
	Connection *model.Connection
	Secrets    map[string]string
	// CrewID and ScheduleID are whose watch this is (tracking_list sources read the crew's tracking list)
	CrewID     string
	ScheduleID string
	// Previous is what the last snapshot remembered about each item (tracker sources); empty on a first check
	Previous map[string]ItemMeta
	// FirstCheck is true when there is no previous snapshot
	FirstCheck bool
	Now        time.Time
}

// Fetched is what one fetch returned: a readable label for the change list, and the items.
type Fetched struct {
	Label string
	Items []Item
	// Comments are new comments since the last check (tracker sources), and how many were past the cap
	Comments        []Comment
	CommentsOmitted int
	// CommentCounts are how many new comments each item got, before the per-check cap
	CommentCounts map[string]int
	// Groups are the tracking list's items with what is below them, for roll-ups (tracking_list sources)
	Groups []Group
}

// Source reads one kind of source.
type Source interface {
	Fetch(ctx context.Context, req Request) (Fetched, error)
}

// readCapped reads at most limit bytes, failing (rather than cutting the data) when there is more.
func readCapped(r io.Reader, limit int64, what string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, sourceErrorf("could not read %s: %v", what, unwrapNetError(err))
	}
	if int64(len(data)) > limit {
		return nil, sourceErrorf("%s is larger than %d MB", what, limit>>20)
	}
	return data, nil
}

// unwrapNetError keeps errors general, so a watch cannot be used to probe a network: a refused private address
// reads the same as a server that is down.
func unwrapNetError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}
	return "the connection failed (the server is down, or the address is private and not allowed)"
}

// errBlockedAddress is returned when a source would connect to an address it must not reach.
var errBlockedAddress = errors.New("address not allowed")

func unsupported(kind string) error {
	return sourceErrorf("watch sources of kind %q are not supported", kind)
}

// FetchItems runs the source for req.Def's kind.
func FetchItems(ctx context.Context, sources map[string]Source, req Request) (Fetched, error) {
	src, ok := sources[req.Def.Source.Kind]
	if !ok {
		return Fetched{}, unsupported(req.Def.Source.Kind)
	}
	fetched, err := src.Fetch(ctx, req)
	if err != nil {
		var limit *LimitError
		var serr *SourceError
		if errors.As(err, &limit) || errors.As(err, &serr) {
			return Fetched{}, err
		}
		return Fetched{}, fmt.Errorf("fetch %s source: %w", req.Def.Source.Kind, err)
	}
	return fetched, nil
}
