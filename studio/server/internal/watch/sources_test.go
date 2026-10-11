package watch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"crewstudio/internal/connections"
	"crewstudio/internal/model"
)

func httpDef(path, items string) model.WatchInput {
	return model.WatchInput{Source: model.WatchSource{Kind: model.WatchSourceHTTPJSON, Path: path, ItemsPath: items},
		IDField: "key", TrackedFields: []string{"status"}, MaxItems: 100}
}

func TestHTTPJSONFetch(t *testing.T) {
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("X-Key"), r.URL.RequestURI()
		switch r.URL.Path {
		case "/api/issues":
			_, _ = w.Write([]byte(`{"items": [{"key": "A", "status": "Open"}]}`))
		case "/api/denied":
			w.WriteHeader(http.StatusForbidden)
		case "/api/moved":
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		case "/api/big":
			_, _ = w.Write([]byte(`[` + strings.Repeat(`{"key":"x"},`, 1) + `{"key":"y"}]`))
		default:
			_, _ = w.Write([]byte("not json"))
		}
	}))
	defer srv.Close()
	conn := &model.Connection{Name: "Tracker API", Kind: "http", Config: map[string]string{"base_url": srv.URL + "/api/", "auth_header": "X-Key"}}
	src := HTTPJSON{Transport: connections.Transport()}
	req := Request{Def: httpDef("/issues?state=open", "items"), Connection: conn, Secrets: map[string]string{"auth_value": "s3cret"}}
	got, err := src.Fetch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "s3cret" || gotPath != "/api/issues?state=open" || len(got.Items) != 1 || got.Label != "Tracker API: /issues?state=open" {
		t.Errorf("fetched %+v (auth %q path %q)", got, gotAuth, gotPath)
	}

	cases := map[string]struct {
		req  Request
		want string
	}{
		"forbidden":  {Request{Def: httpDef("/denied", ""), Connection: conn, Secrets: req.Secrets}, "HTTP 403 (check the connection's credentials)"},
		"redirect":   {Request{Def: httpDef("/moved", ""), Connection: conn, Secrets: req.Secrets}, "HTTP 302 (a redirect"},
		"not json":   {Request{Def: httpDef("/other", ""), Connection: conn, Secrets: req.Secrets}, "not valid JSON"},
		"no secret":  {Request{Def: httpDef("/issues", "items"), Connection: conn}, "no saved value"},
		"no conn":    {Request{Def: httpDef("/issues", "items")}, "needs an http connection"},
		"over limit": {Request{Def: model.WatchInput{Source: model.WatchSource{Path: "/big"}, IDField: "key", MaxItems: 1}, Connection: conn, Secrets: req.Secrets}, "limit of 1"},
	}
	for name, tc := range cases {
		if _, err := src.Fetch(context.Background(), tc.req); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
	unreachable := &model.Connection{Name: "Gone", Kind: "http", Config: map[string]string{"base_url": "http://127.0.0.1:1"}}
	if _, err := src.Fetch(context.Background(), Request{Def: httpDef("/x", ""), Connection: unreachable}); err == nil || !strings.Contains(err.Error(), "could not reach Gone") {
		t.Errorf("unreachable: %v", err)
	}
}

func TestRequestURLStaysOnTheHost(t *testing.T) {
	if u, err := requestURL("https://api.example.com/v1/", "/issues?x=1"); err != nil || u != "https://api.example.com/v1/issues?x=1" {
		t.Errorf("join = %q %v", u, err)
	}
	for _, p := range []string{"@evil.com/x", ".evil.com/x", "/../admin", "/%2e%2e/admin"} {
		if _, err := requestURL("https://api.example.com", p); err == nil {
			t.Errorf("%q left the host", p)
		}
	}
	if _, err := requestURL("not a url", "/x"); err == nil {
		t.Error("bad base accepted")
	}
}

func pageDef(url, selector string) model.WatchInput {
	return model.WatchInput{Source: model.WatchSource{Kind: model.WatchSourceWebPage, URL: url, Selector: selector}}
}

func TestWebPageFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><head><title>x</title><style>p{}</style></head><body>
				<nav>Menu</nav><main id="s"><h1>Status</h1><p>All   systems
				operational</p><script>var x = 1</script></main></body></html>`))
		case "/plain":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("  line one \n\n line two "))
		case "/bare":
			_, _ = w.Write([]byte("<!DOCTYPE html><html><body><p>bare</p></body></html>"))
		case "/image":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 'P', 'N', 'G'})
		case "/hop":
			http.Redirect(w, r, "/status", http.StatusFound)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	page := WebPage{Transport: srv.Client().Transport}
	fetch := func(path, selector string) (Fetched, error) {
		return page.Fetch(context.Background(), Request{Def: pageDef(srv.URL+path, selector)})
	}
	got, err := fetch("/status?token=s3cret#top", "#s")
	if err != nil {
		t.Fatal(err)
	}
	if got.Items[0].Fields["text"] != "Status\nAll systems operational" || got.Label != srv.URL+"/status (#s)" || got.Items[0].ID != srv.URL+"/status" {
		t.Errorf("fetched %+v", got)
	}
	if got, err := fetch("/hop", ""); err != nil || !strings.Contains(got.Items[0].Fields["text"].(string), "Menu") {
		t.Errorf("redirect: %+v %v", got, err)
	}
	if got, _ := fetch("/plain", ""); got.Items[0].Fields["text"] != "line one\nline two" {
		t.Errorf("plain = %+v", got.Items)
	}
	if got, _ := fetch("/bare", ""); got.Items[0].Fields["text"] != "bare" {
		t.Errorf("bare = %+v", got.Items)
	}
	for path, want := range map[string]string{"/missing": "HTTP 404", "/image": "not HTML or text", "/loop": "could not fetch"} {
		if _, err := fetch(path, ""); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", path, err)
		}
	}
	if _, err := fetch("/status", "#nothing"); err == nil || !strings.Contains(err.Error(), "matched nothing") {
		t.Errorf("no match: %v", err)
	}
	if _, err := fetch("/plain", "p"); err == nil || !strings.Contains(err.Error(), "plain text") {
		t.Errorf("selector on text: %v", err)
	}
	if _, err := fetch("/status", "p[["); err == nil {
		t.Error("bad selector accepted")
	}

	// The real transport refuses this loopback test server
	public := WebPage{Timeout: 5 * time.Second}
	if _, err := public.Fetch(context.Background(), Request{Def: pageDef(srv.URL+"/status", "")}); err == nil || !strings.Contains(err.Error(), "not allowed") || !strings.Contains(err.Error(), "the server is down") {
		t.Errorf("loopback fetch: %v", err)
	}
}

func TestIsPublic(t *testing.T) {
	for addr, want := range map[string]bool{
		"8.8.8.8": true, "2606:4700:4700::1111": true,
		"127.0.0.1": false, "10.1.2.3": false, "192.168.1.1": false, "172.16.0.1": false, "169.254.169.254": false,
		"100.64.0.1": false, "0.0.0.0": false, "::1": false, "fe80::1": false, "fc00::1": false, "::ffff:127.0.0.1": false,
		"64:ff9b::a00:1": false, "2002:a00:1::": false, "224.0.0.1": false, "198.18.0.1": false,
		"::7f00:1": false, "fec0::1": false, "192.88.99.1": false,
	} {
		if got := IsPublic(netip.MustParseAddr(addr)); got != want {
			t.Errorf("IsPublic(%s) = %v", addr, got)
		}
	}
	if err := publicOnly("tcp", "8.8.8.8:443", nil); err != nil {
		t.Errorf("public refused: %v", err)
	}
	for _, a := range []string{"127.0.0.1:80", "nonsense", "example.com:80"} {
		if err := publicOnly("tcp", a, nil); !errors.Is(err, errBlockedAddress) {
			t.Errorf("%s allowed", a)
		}
	}
}

func TestPageTextAndSelectors(t *testing.T) {
	if CheckSelector("") != "" || CheckSelector("div.status > p") != "" || CheckSelector("p[[") == "" {
		t.Error("CheckSelector")
	}
	text, err := PageText([]byte(`<ul><li>one</li><li>two</li></ul><!-- c --><template>t</template>`), "li")
	if err != nil || text != "one\ntwo" {
		t.Errorf("text = %q %v", text, err)
	}
}

func fileDef(file, format, items string, tracked ...string) model.WatchInput {
	return model.WatchInput{Source: model.WatchSource{Kind: model.WatchSourceWorkspaceFile, File: file, Format: format, ItemsPath: items},
		IDField: "id", TrackedFields: tracked, MaxItems: 3}
}

func TestWorkspaceFileFetch(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	write := func(name, body string) {
		t.Helper()
		p := filepath.Join(ws, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("inbox/courses.csv", "\xef\xbb\xbfid,due,owner\nC1,2026-10-10,sam\nC2,2026-10-12\n")
	write("inbox/list.json", `{"items": [{"id": "A", "due": "2026-10-10"}]}`)
	write("inbox/dupes.csv", "id,due\nA,1\nA,2\n")
	write("inbox/many.csv", "id,due\n1,a\n2,b\n3,c\n4,d\n")
	write("inbox/empty.csv", "")
	write("inbox/badrow.csv", "id,due\n,x\n")
	write("inbox/quote.csv", "id,due\n\"A,1\n")
	if err := os.WriteFile(filepath.Join(dir, "secret.json"), []byte(`[{"id":"leak"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "secret.json"), filepath.Join(ws, "inbox", "link.json")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(ws, "inbox", "pipe.csv"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := WorkspaceFile{Dir: ws}
	fetch := func(def model.WatchInput) (Fetched, error) { return src.Fetch(context.Background(), Request{Def: def}) }

	got, err := fetch(fileDef("inbox/courses.csv", "csv", "", "due", "owner"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 || got.Items[1].Fields["owner"] != "" || got.Items[0].Fields["due"] != "2026-10-10" || got.Label != "workspace:inbox/courses.csv" {
		t.Errorf("csv = %+v", got)
	}
	if got, err := fetch(fileDef("inbox/list.json", "json", "items", "due")); err != nil || got.Items[0].ID != "A" {
		t.Errorf("json = %+v %v", got, err)
	}
	cases := map[string]struct {
		def  model.WatchInput
		want string
	}{
		"missing column": {fileDef("inbox/courses.csv", "csv", "", "status"), `no column "status"`},
		"duplicate ids":  {fileDef("inbox/dupes.csv", "csv", "", "due"), "items 1 and 2 have the same id"},
		"too many":       {fileDef("inbox/many.csv", "csv", "", "due"), "limit of 3"},
		"empty":          {fileDef("inbox/empty.csv", "csv", "", "due"), "empty"},
		"bad row":        {fileDef("inbox/badrow.csv", "csv", "", "due"), "row 2: the id is empty"},
		"bad quote":      {fileDef("inbox/quote.csv", "csv", "", "due"), "could not be read"},
		"no file":        {fileDef("inbox/nope.csv", "csv", "", "due"), "there is no file"},
		"escape":         {fileDef("../secret.json", "json", "", "due"), "cannot be opened"},
		"symlink out":    {fileDef("inbox/link.json", "json", "", "due"), "not a regular file"},
		"named pipe":     {fileDef("inbox/pipe.csv", "csv", "", "due"), "not a regular file"},
		"directory":      {fileDef("inbox", "json", "", "due"), "not a regular file"},
		"bad json":       {fileDef("inbox/courses.csv", "json", "", "due"), "not valid JSON"},
	}
	for name, tc := range cases {
		if _, err := fetch(tc.def); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
	if _, err := (WorkspaceFile{}).Fetch(context.Background(), Request{Def: fileDef("a.csv", "csv", "", "due")}); err == nil {
		t.Error("no workspace accepted")
	}
	if _, err := (WorkspaceFile{Dir: filepath.Join(dir, "none")}).Fetch(context.Background(), Request{Def: fileDef("a.csv", "csv", "", "due")}); err == nil {
		t.Error("missing workspace accepted")
	}
}

type fakeSource struct {
	fetched Fetched
	err     error
}

func (f fakeSource) Fetch(context.Context, Request) (Fetched, error) { return f.fetched, f.err }

func TestFetchItemsWrapsErrors(t *testing.T) {
	sources := map[string]Source{"ok": fakeSource{fetched: Fetched{Label: "x"}}, "boom": fakeSource{err: errors.New("db down")},
		"limit": fakeSource{err: &LimitError{Count: 3, Max: 2}}}
	if got, err := FetchItems(context.Background(), sources, Request{Def: model.WatchInput{Source: model.WatchSource{Kind: "ok"}}}); err != nil || got.Label != "x" {
		t.Errorf("ok: %v", err)
	}
	if _, err := FetchItems(context.Background(), sources, Request{Def: model.WatchInput{Source: model.WatchSource{Kind: "boom"}}}); err == nil || !strings.Contains(err.Error(), "fetch boom source") {
		t.Errorf("boom: %v", err)
	}
	var limit *LimitError
	if _, err := FetchItems(context.Background(), sources, Request{Def: model.WatchInput{Source: model.WatchSource{Kind: "limit"}}}); !errors.As(err, &limit) {
		t.Errorf("limit: %v", err)
	}
	if _, err := FetchItems(context.Background(), sources, Request{Def: model.WatchInput{Source: model.WatchSource{Kind: "jira"}}}); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Errorf("unknown: %v", err)
	}
	if unwrapNetError(context.DeadlineExceeded) != "timed out" {
		t.Error("deadline wording")
	}
	if _, err := readCapped(strings.NewReader("12345"), 4, "the body"); err == nil {
		t.Error("over the cap accepted")
	}
}
