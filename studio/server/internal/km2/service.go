package km2

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"crewstudio/internal/connections"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

const (
	testTimeout = 20 * time.Second
	// Per connection, for calls agents make during runs (each run is also limited on its own, see api/runkm2.go), so
	// crews together cannot flood the key they share
	runCallsPerSecond = 5
	runCallBurst      = 40
	// maxTestFolders is how many folders a test lists for people
	maxTestFolders = 200
)

// Service builds KM2 clients for connections, opening their keys on the server. Keys never leave it.
type Service struct {
	store *store.Store
	conns *connections.Service
	opts  Options

	mu    sync.Mutex
	calls map[string]*rate.Limiter
}

// NewService builds the service.
func NewService(st *store.Store, conns *connections.Service, opts Options) *Service {
	return &Service{store: st, conns: conns, opts: opts, calls: map[string]*rate.Limiter{}}
}

// Allow reports whether a run may make another KM2 call through the connection now.
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

// Client opens a connection's key and builds its client.
func (s *Service) Client(ctx context.Context, conn model.Connection) (*Client, error) {
	if conn.Kind != model.KindKM2 {
		return nil, errorf("%s is a %s connection, not a KM2 one", conn.Name, conn.Kind)
	}
	secrets, err := s.conns.Secrets(ctx, conn.ID)
	if err != nil {
		return nil, errorf("the key of %s cannot be read on the server", conn.Name)
	}
	return New(conn, secrets, s.opts)
}

// UseProblem says why a crew cannot use a connection for KM2, or "".
func UseProblem(use store.CrewConnectionUse) string {
	c := use.Connection
	switch {
	case c.Kind != model.KindKM2:
		return fmt.Sprintf("the connection %s is a %s connection, not a KM2 one", c.Name, c.Kind)
	case !c.Enabled:
		return fmt.Sprintf("the connection %s is disabled", c.Name)
	case !use.Linked:
		return fmt.Sprintf("the crew may not use the connection %s (choose it on the crew's State tab)", c.Name)
	case !use.SharesArea:
		return fmt.Sprintf("the connection %s shares no functional area with the crew", c.Name)
	}
	return ""
}

// ForAgentChat loads a KM2 connection an agent-chat turn froze into its spec and builds its client. Such a turn has
// no crew, so there is no crew link or crew area to check: the run-state API has already checked that the turn froze
// the connection and that the person chatting can still see it. The connection must still be enabled and of the
// right kind.
func (s *Service) ForAgentChat(ctx context.Context, connectionID string) (model.Connection, *Client, error) {
	c, err := s.store.GetConnection(ctx, connectionID)
	if errors.Is(err, store.ErrNotFound) {
		return model.Connection{}, nil, errorf("the KM2 connection no longer exists")
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

// ForCrew loads a KM2 connection a crew may use and builds its client.
func (s *Service) ForCrew(ctx context.Context, connectionID, crewID string) (model.Connection, *Client, error) {
	use, err := s.store.ConnectionForCrew(ctx, connectionID, crewID)
	if errors.Is(err, store.ErrNotFound) {
		return model.Connection{}, nil, errorf("the KM2 connection no longer exists")
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

// TestResult is a connection test with the folders the key can see.
type TestResult struct {
	connections.TestResult
	// Folders are the folders the key can see (the first 200)
	Folders []Folder `json:"folders,omitempty"`
	// FolderCount is how many folders the key can see
	FolderCount int `json:"folder_count"`
	// Warnings are problems with the connection's folder settings
	Warnings []string `json:"warnings,omitempty"`
}

// Test checks a KM2 connection with a cheap authenticated call: the folder list.
func (s *Service) Test(ctx context.Context, conn model.Connection) TestResult {
	list := s.FolderList(ctx, conn)
	res := TestResult{TestResult: list.TestResult, FolderCount: list.FolderCount, Warnings: list.Warnings}
	if list.OK {
		res.Folders = list.Folders[:min(len(list.Folders), maxTestFolders)]
		res.Message = fmt.Sprintf("Connected: the key sees %d folders.", list.FolderCount)
		res.Warnings = append(res.Warnings, plainHTTPWarning(conn.Config["base_url"])...)
	}
	return res
}

// AllowedFolders are the folders a connection lets crews save to.
type AllowedFolders struct {
	// DefaultFolderID is "" when saving without naming a folder is off
	DefaultFolderID string   `json:"default_folder_id"`
	WriteFolderIDs  []string `json:"write_folder_ids"`
}

// FolderList is every folder a connection's key can see, for pickers, with the folders it allows.
type FolderList struct {
	connections.TestResult
	Folders     []Folder       `json:"folders"`
	FolderCount int            `json:"folder_count"`
	Allowed     AllowedFolders `json:"allowed"`
	Warnings    []string       `json:"warnings"`
}

// AllowedOf reads the folders a connection's settings allow.
func AllowedOf(config map[string]string) AllowedFolders {
	out := AllowedFolders{DefaultFolderID: config[model.ConfigKM2DefaultFolder], WriteFolderIDs: []string{}}
	for _, id := range strings.Split(config[model.ConfigKM2WriteFolders], ",") {
		if id = strings.TrimSpace(id); id != "" && !slices.Contains(out.WriteFolderIDs, id) {
			out.WriteFolderIDs = append(out.WriteFolderIDs, id)
		}
	}
	return out
}

// FolderList reads the folders the connection's key can see (KM2 needs knowledge:read). A key that may not list
// folders, or a KM2 that fails, gives ok false and a message people can act on.
func (s *Service) FolderList(ctx context.Context, conn model.Connection) FolderList {
	res := FolderList{TestResult: connections.TestResult{Supported: true}, Folders: []Folder{}, Allowed: AllowedOf(conn.Config), Warnings: []string{}}
	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()
	start := time.Now()
	client, err := s.Client(ctx, conn)
	var folders []Folder
	if err == nil {
		folders, err = client.Folders(ctx)
	}
	res.DurationMS = time.Since(start).Milliseconds()
	res.StatusCode = StatusOf(err)
	switch {
	case res.StatusCode == 403:
		res.Message = "KM2 accepted the key, but it may not list folders. Grant it knowledge:read so Test and folder names work; " +
			"check that it also holds search:read and knowledge:write."
	case err != nil:
		res.Message = "Could not use the connection: " + SafeMessage(err)
	default:
		res.OK = true
		res.FolderCount = len(folders)
		res.Folders = folders
		res.Message = fmt.Sprintf("The key sees %d folders.", len(folders))
		res.Warnings = folderWarnings(conn.Config, folders)
	}
	return res
}

// plainHTTPWarning warns when the key would cross the network unencrypted (http to anything but this machine).
func plainHTTPWarning(base string) []string {
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" {
		return nil
	}
	host := u.Hostname()
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return []string{"The base URL uses http, so the key crosses the network unencrypted. Use https unless KM2 is on a trusted network."}
}

// folderWarnings checks the connection's save folders against what the key can see.
func folderWarnings(config map[string]string, visible []Folder) []string {
	out := []string{}
	allowed := model.KM2Folders(config)
	switch {
	case len(allowed) == 0:
		out = append(out, "No folders are set for saving, so crews cannot save documents through this connection.")
	case config[model.ConfigKM2DefaultFolder] == "":
		out = append(out, "No default folder is set, so crews must name one of the other folders when they save.")
	}
	for _, id := range allowed {
		if !slices.ContainsFunc(visible, func(f Folder) bool { return f.ID == id }) {
			out = append(out, fmt.Sprintf("The key cannot see folder %s, so saves to it will fail.", id))
		}
	}
	return out
}
