package connections

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/secrets"
	"crewstudio/internal/store"
	"crewstudio/internal/testdb"
)

const areaFinance = "55555555-0000-4000-8000-000000000001"

func newService(t *testing.T, withKey bool) (*Service, *store.Store) {
	t.Helper()
	st := store.New(testdb.New(t, "connections_test"))
	var box *secrets.Box
	if withKey {
		var err error
		if box, err = secrets.NewBox(secrets.GenerateKey()); err != nil {
			t.Fatal(err)
		}
	}
	return New(st, box), st
}

func httpConn(baseURL string) model.ConnectionInput {
	return model.ConnectionInput{Name: "api", Kind: "http", AreaIDs: []string{areaFinance},
		Config: map[string]string{"base_url": baseURL, "auth_header": "X-API-Key"}}
}

func TestCreateSealsSecretsAndReplaceSwapsThem(t *testing.T) {
	svc, st := newService(t, true)
	ctx := context.Background()
	in := httpConn("https://api.example")
	in.Secrets = map[string]string{"auth_value": "first-secret"}
	c, err := svc.Create(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.SecretKeys) != 1 || c.SecretKeys[0] != "auth_value" {
		t.Errorf("created = %+v", c)
	}
	cipher, _ := st.ConnectionCipher(ctx, c.ID)
	if len(cipher) == 0 || strings.Contains(string(cipher), "first-secret") {
		t.Fatal("secret is not encrypted at rest")
	}
	got, err := svc.Secrets(ctx, c.ID)
	if err != nil || got["auth_value"] != "first-secret" {
		t.Fatalf("secrets = %v, %v", got, err)
	}
	if _, err := svc.ReplaceSecrets(ctx, c.ID, map[string]string{"auth_value": "second"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Secrets(ctx, c.ID); got["auth_value"] != "second" {
		t.Errorf("after replace = %v", got)
	}
	if _, err := svc.ReplaceSecrets(ctx, "99999999-0000-4000-8000-000000000000", map[string]string{"auth_value": "x"}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
	// A connection without secrets reads as an empty set
	plain := httpConn("https://plain.example")
	plain.Name = "plain"
	pc, _ := svc.Create(ctx, plain)
	if got, err := svc.Secrets(ctx, pc.ID); err != nil || len(got) != 0 {
		t.Errorf("plain secrets = %v, %v", got, err)
	}
}

func TestWithoutAKeyNothingSecretCanBeSaved(t *testing.T) {
	svc, _ := newService(t, false)
	if svc.Ready() {
		t.Fatal("no key: not ready")
	}
	in := httpConn("https://api.example")
	if _, err := svc.Create(context.Background(), in); !isNoKey(err) {
		t.Errorf("create: %v", err)
	}
	if _, err := svc.ReplaceSecrets(context.Background(), "x", map[string]string{"a": "b"}); !isNoKey(err) {
		t.Errorf("replace: %v", err)
	}
	if _, err := svc.Secrets(context.Background(), "x"); !isNoKey(err) {
		t.Errorf("read: %v", err)
	}
}

func isNoKey(err error) bool {
	var c *store.ConflictError
	return errors.As(err, &c) && c.Code == "secrets_key_missing"
}

func TestCheckKeyAtStartup(t *testing.T) {
	ctx := context.Background()
	st := store.New(testdb.New(t, "connections_test"))
	if box, err := CheckKey(ctx, st, ""); err != nil || box != nil {
		t.Fatalf("no connections, no key: %v %v", box, err)
	}
	key := secrets.GenerateKey()
	box, err := CheckKey(ctx, st, key)
	if err != nil || box == nil {
		t.Fatalf("key: %v", err)
	}
	if _, err := CheckKey(ctx, st, "not a key"); err == nil || !strings.Contains(err.Error(), "STUDIO_SECRETS_KEY") {
		t.Errorf("bad key: %v", err)
	}
	in := httpConn("https://api.example")
	in.Secrets = map[string]string{"auth_value": "s"}
	if _, err := New(st, box).Create(ctx, in); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckKey(ctx, st, ""); err == nil || !strings.Contains(err.Error(), "STUDIO_SECRETS_KEY is not set") {
		t.Errorf("missing key with connections: %v", err)
	}
	if _, err := CheckKey(ctx, st, secrets.GenerateKey()); err == nil || !strings.Contains(err.Error(), "does not decrypt") {
		t.Errorf("wrong key: %v", err)
	}
	if _, err := CheckKey(ctx, st, key); err != nil {
		t.Errorf("right key: %v", err)
	}
}

func TestTestHTTPConnection(t *testing.T) {
	svc, _ := newService(t, true)
	ctx := context.Background()
	var lastKey atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastKey.Store(r.Header.Get("X-API-Key"))
		switch r.URL.Path {
		case "/denied":
			w.WriteHeader(http.StatusUnauthorized)
		case "/moved":
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		case "/slow":
			time.Sleep(300 * time.Millisecond)
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer srv.Close()
	svc.timeout = 100 * time.Millisecond

	in := httpConn(srv.URL)
	in.Secrets = map[string]string{"auth_value": "k-123"}
	c, err := svc.Create(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	res := svc.Test(ctx, c)
	if !res.Supported || !res.OK || res.StatusCode != 200 || lastKey.Load() != "k-123" {
		t.Errorf("ok = %+v (key %q)", res, lastKey.Load())
	}
	for path, want := range map[string]string{"/denied": "refused the credentials", "/moved": "redirect", "/slow": "Could not reach"} {
		c.Config = map[string]string{"base_url": srv.URL + path, "auth_header": "X-API-Key"}
		res := svc.Test(ctx, c)
		if res.OK || !strings.Contains(res.Message, want) {
			t.Errorf("%s = %+v", path, res)
		}
	}
	c.Config = map[string]string{"base_url": srv.URL, "auth_header": "X-Other"}
	if res := svc.Test(ctx, c); !res.OK || lastKey.Load() != "" {
		t.Errorf("other header = %+v", res)
	}
	if res := svc.Test(ctx, model.Connection{Kind: "email"}); res.Supported || !strings.Contains(res.Message, "delivery client") {
		t.Errorf("email = %+v", res)
	}
	if res := svc.Test(ctx, model.Connection{Kind: "jira"}); res.Supported || !strings.Contains(res.Message, "tracker client") {
		t.Errorf("jira = %+v", res)
	}
	noSecret := model.Connection{ID: "99999999-0000-4000-8000-000000000000", Kind: "http",
		Config: map[string]string{"base_url": srv.URL, "auth_header": "X-API-Key"}}
	if res := svc.Test(ctx, noSecret); res.OK || !strings.Contains(res.Message, "no auth header value") {
		t.Errorf("no secret = %+v", res)
	}
}

func TestTestRefusesLinkLocalAddresses(t *testing.T) {
	svc, _ := newService(t, true)
	res := svc.Test(context.Background(), model.Connection{Kind: "http", Config: map[string]string{"base_url": "http://169.254.169.254/latest"}})
	if res.OK || !strings.Contains(res.Message, "not allowed") {
		t.Errorf("metadata address = %+v", res)
	}
	res = svc.Test(context.Background(), model.Connection{Kind: "http", Config: map[string]string{"base_url": "http://127.0.0.1:1/"}})
	if res.OK || !strings.Contains(res.Message, "could not connect") {
		t.Errorf("closed port = %+v", res)
	}
	if res := svc.Test(context.Background(), model.Connection{Kind: "http", Config: map[string]string{"base_url": "::nope"}}); res.OK || !strings.Contains(res.Message, "not usable") {
		t.Errorf("bad url = %+v", res)
	}
}
