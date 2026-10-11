package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

const testClientID = "studio-client"

// fakeIdP is a minimal OpenID Connect provider: discovery, JWKS, token and userinfo endpoints.
type fakeIdP struct {
	t   *testing.T
	srv *httptest.Server
	key *rsa.PrivateKey

	mu sync.Mutex
	// per authorization code: PKCE challenge and nonce from the authorization URL
	codes map[string]codeInfo
	// knobs
	extraClaims    map[string]any
	nonceOverride  *string
	audience       string
	expiresIn      time.Duration
	signWithOther  bool
	omitIDToken    bool
	userinfo       map[string]any
	userinfoStatus int
	discoveryIss   string
	tokenRequests  int
}

type codeInfo struct{ challenge, nonce string }

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIdP{t: t, key: key, codes: map[string]codeInfo{}, audience: testClientID, expiresIn: time.Hour, userinfoStatus: 200}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", f.discovery)
	mux.HandleFunc("/jwks", f.jwks)
	mux.HandleFunc("/token", f.token)
	mux.HandleFunc("/userinfo", f.userinfoHandler)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIdP) issuer() string { return f.srv.URL }

func (f *fakeIdP) discovery(w http.ResponseWriter, _ *http.Request) {
	iss := f.srv.URL
	if f.discoveryIss != "" {
		iss = f.discoveryIss
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"issuer": iss, "authorization_endpoint": f.srv.URL + "/authorize", "token_endpoint": f.srv.URL + "/token",
		"jwks_uri": f.srv.URL + "/jwks", "userinfo_endpoint": f.srv.URL + "/userinfo",
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (f *fakeIdP) jwks(w http.ResponseWriter, _ *http.Request) {
	set := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &f.key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}}
	_ = json.NewEncoder(w).Encode(set)
}

func (f *fakeIdP) userinfoHandler(w http.ResponseWriter, _ *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.userinfoStatus != 200 {
		http.Error(w, "nope", f.userinfoStatus)
		return
	}
	_ = json.NewEncoder(w).Encode(f.userinfo)
}

func (f *fakeIdP) token(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokenRequests++
	_ = r.ParseForm()
	info, ok := f.codes[r.PostForm.Get("code")]
	if !ok || r.PostForm.Get("grant_type") != "authorization_code" {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != info.challenge {
		http.Error(w, `{"error":"invalid_grant","error_description":"PKCE verification failed"}`, http.StatusBadRequest)
		return
	}
	delete(f.codes, r.PostForm.Get("code")) // single use
	resp := map[string]any{"access_token": "at-123", "token_type": "Bearer", "expires_in": 3600}
	if !f.omitIDToken {
		resp["id_token"] = f.signIDToken(info.nonce)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (f *fakeIdP) signIDToken(nonce string) string {
	if f.nonceOverride != nil {
		nonce = *f.nonceOverride
	}
	claims := map[string]any{"iss": f.srv.URL, "aud": f.audience, "sub": "subj-1", "iat": time.Now().Unix(),
		"exp": time.Now().Add(f.expiresIn).Unix(), "nonce": nonce}
	for k, v := range f.extraClaims {
		claims[k] = v
	}
	key := f.key
	if f.signWithOther {
		key, _ = rsa.GenerateKey(rand.Reader, 2048)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: key, KeyID: "k1"}}, nil)
	if err != nil {
		f.t.Fatal(err)
	}
	payload, _ := json.Marshal(claims)
	obj, err := signer.Sign(payload)
	if err != nil {
		f.t.Fatal(err)
	}
	s, err := obj.CompactSerialize()
	if err != nil {
		f.t.Fatal(err)
	}
	return s
}

// authorize plays the browser: it reads the authorization URL and returns a code the token endpoint will accept.
func (f *fakeIdP) authorize(authURL string) string {
	f.t.Helper()
	u, err := url.Parse(authURL)
	if err != nil {
		f.t.Fatal(err)
	}
	q := u.Query()
	f.mu.Lock()
	defer f.mu.Unlock()
	code := RandomToken(8)
	f.codes[code] = codeInfo{challenge: q.Get("code_challenge"), nonce: q.Get("nonce")}
	return code
}

func (f *fakeIdP) set(fn func(f *fakeIdP)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func newTestSSO(f *fakeIdP, key string, mutate func(*ProviderConfig)) *SSO {
	cfg := ProviderConfig{Key: key, Label: key, Issuer: f.issuer(), ClientID: testClientID, ClientSecret: "sekret"}
	if mutate != nil {
		mutate(&cfg)
	}
	return NewSSO("https://studio.example.com", cfg)
}

func TestSSOBeginBuildsAuthorizationURL(t *testing.T) {
	f := newFakeIdP(t)
	s := newTestSSO(f, ProviderClerk, func(c *ProviderConfig) { c.AuthParams = map[string]string{"prompt": "select_account"} })
	a, err := s.Begin(context.Background(), ProviderClerk)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Begin(context.Background(), ProviderClerk)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(a.URL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if !strings.HasPrefix(a.URL, f.issuer()+"/authorize?") {
		t.Errorf("url = %s", a.URL)
	}
	checks := map[string]string{
		"client_id": testClientID, "response_type": "code", "state": a.State, "nonce": a.Nonce,
		"code_challenge_method": "S256", "prompt": "select_account",
		"redirect_uri": "https://studio.example.com/api/auth/clerk/callback",
	}
	for k, want := range checks {
		if q.Get(k) != want {
			t.Errorf("param %s = %q, want %q", k, q.Get(k), want)
		}
	}
	sum := sha256.Sum256([]byte(a.CodeVerifier))
	if q.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(sum[:]) {
		t.Error("code_challenge is not S256 of the verifier")
	}
	for _, sc := range []string{"openid", "profile", "email"} {
		if !strings.Contains(q.Get("scope"), sc) {
			t.Errorf("scope %q lacks %s", q.Get("scope"), sc)
		}
	}
	if a.State == b.State || a.Nonce == b.Nonce || a.CodeVerifier == b.CodeVerifier || a.State == a.Nonce {
		t.Error("state, nonce and verifier must be fresh per login")
	}
	if len(a.State) < 40 {
		t.Errorf("state too short: %d", len(a.State))
	}
}

func TestSSOBeginErrors(t *testing.T) {
	f := newFakeIdP(t)
	s := newTestSSO(f, ProviderClerk, nil)
	if _, err := s.Begin(context.Background(), "google"); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Errorf("unknown provider: %v", err)
	}
	if _, err := s.Complete(context.Background(), "google", "c", "v", "n"); err == nil {
		t.Error("complete with unknown provider should fail")
	}

	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	s2 := NewSSO("https://x", ProviderConfig{Key: ProviderClerk, Label: "Clerk", Issuer: dead.URL, ClientID: "c"})
	if _, err := s2.Begin(context.Background(), ProviderClerk); err == nil || !strings.Contains(err.Error(), "discover") {
		t.Errorf("unreachable issuer: %v", err)
	}

	f.set(func(f *fakeIdP) { f.discoveryIss = "https://evil.example" })
	s3 := newTestSSO(f, ProviderClerk, nil)
	if _, err := s3.Begin(context.Background(), ProviderClerk); err == nil {
		t.Error("issuer mismatch in discovery document must fail")
	}
}

func TestSSODiscoveryRetriedAfterFailure(t *testing.T) {
	f := newFakeIdP(t)
	f.set(func(f *fakeIdP) { f.discoveryIss = "https://evil.example" })
	s := newTestSSO(f, ProviderClerk, nil)
	if _, err := s.Begin(context.Background(), ProviderClerk); err == nil {
		t.Fatal("want failure")
	}
	f.set(func(f *fakeIdP) { f.discoveryIss = "" })
	if _, err := s.Begin(context.Background(), ProviderClerk); err != nil {
		t.Errorf("a failed discovery must not be cached: %v", err)
	}
}

func completeFlow(t *testing.T, f *fakeIdP, s *SSO, key string) (Identity, error) {
	t.Helper()
	ctx := context.Background()
	start, err := s.Begin(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	code := f.authorize(start.URL)
	return s.Complete(ctx, key, code, start.CodeVerifier, start.Nonce)
}

func TestSSOCompleteClerkHappyPath(t *testing.T) {
	f := newFakeIdP(t)
	f.set(func(f *fakeIdP) {
		f.extraClaims = map[string]any{"email": "Ada@Example.com", "email_verified": true, "name": " Ada L "}
	})
	id, err := completeFlow(t, f, newTestSSO(f, ProviderClerk, nil), ProviderClerk)
	if err != nil {
		t.Fatal(err)
	}
	if id.Provider != ProviderClerk || id.Subject != "subj-1" || id.Email != "ada@example.com" || id.Name != "Ada L" {
		t.Errorf("identity = %+v", id)
	}
}

func TestSSOCompleteClerkEmailVerifiedFalse(t *testing.T) {
	f := newFakeIdP(t)
	f.set(func(f *fakeIdP) {
		f.extraClaims = map[string]any{"email": "a@b.c", "email_verified": false}
	})
	_, err := completeFlow(t, f, newTestSSO(f, ProviderClerk, nil), ProviderClerk)
	if err != ErrUnverifiedEmail {
		t.Errorf("err = %v", err)
	}
}

func TestSSOCompleteClerkFallsBackToUserinfo(t *testing.T) {
	tests := []struct {
		name     string
		claims   map[string]any
		userinfo map[string]any
		status   int
		wantErr  error
		anyErr   bool
		email    string
	}{
		{"email and verification from userinfo", map[string]any{}, map[string]any{"sub": "subj-1", "email": "U@x.io", "email_verified": true}, 200, nil, false, "u@x.io"},
		{"only verification missing", map[string]any{"email": "id@x.io"}, map[string]any{"sub": "subj-1", "email": "other@x.io", "email_verified": true}, 200, nil, false, "id@x.io"},
		{"userinfo says unverified", map[string]any{}, map[string]any{"sub": "subj-1", "email": "u@x.io", "email_verified": false}, 200, ErrUnverifiedEmail, false, ""},
		{"userinfo subject differs", map[string]any{}, map[string]any{"sub": "someone-else", "email": "u@x.io", "email_verified": true}, 200, ErrUnverifiedEmail, false, ""},
		{"userinfo fails", map[string]any{}, nil, 500, ErrUnverifiedEmail, false, ""},
		{"userinfo has no email either", map[string]any{}, map[string]any{"sub": "subj-1", "email_verified": true}, 200, nil, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeIdP(t)
			f.set(func(f *fakeIdP) { f.extraClaims, f.userinfo, f.userinfoStatus = tt.claims, tt.userinfo, tt.status })
			id, err := completeFlow(t, f, newTestSSO(f, ProviderClerk, nil), ProviderClerk)
			switch {
			case tt.anyErr:
				if err == nil {
					t.Fatal("want error")
				}
			case tt.wantErr != nil:
				if err != tt.wantErr {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
			default:
				if err != nil || id.Email != tt.email {
					t.Fatalf("id = %+v err = %v", id, err)
				}
			}
		})
	}
}

func TestSSOCompleteMicrosoft(t *testing.T) {
	ms := func(c *ProviderConfig) { c.TenantID = "tenant-1" }
	t.Run("tenant and oid make the subject", func(t *testing.T) {
		f := newFakeIdP(t)
		f.set(func(f *fakeIdP) {
			f.extraClaims = map[string]any{"tid": "TENANT-1", "oid": "obj-9", "preferred_username": "Pat@Contoso.com", "email": "evil@x.io", "name": "Pat"}
		})
		id, err := completeFlow(t, f, newTestSSO(f, ProviderMicrosoft, ms), ProviderMicrosoft)
		if err != nil {
			t.Fatal(err)
		}
		if id.Subject != "TENANT-1:obj-9" || id.Email != "pat@contoso.com" {
			t.Errorf("id = %+v", id)
		}
	})
	t.Run("xms_edov true uses email claim", func(t *testing.T) {
		f := newFakeIdP(t)
		f.set(func(f *fakeIdP) {
			f.extraClaims = map[string]any{"tid": "tenant-1", "oid": "o", "preferred_username": "upn@contoso.com", "email": "mail@contoso.com", "xms_edov": true}
		})
		id, err := completeFlow(t, f, newTestSSO(f, ProviderMicrosoft, ms), ProviderMicrosoft)
		if err != nil || id.Email != "mail@contoso.com" {
			t.Errorf("id = %+v %v", id, err)
		}
	})
	t.Run("xms_edov false ignores email claim", func(t *testing.T) {
		f := newFakeIdP(t)
		f.set(func(f *fakeIdP) {
			f.extraClaims = map[string]any{"tid": "tenant-1", "oid": "o", "preferred_username": "upn@contoso.com", "email": "mail@contoso.com", "xms_edov": false}
		})
		id, err := completeFlow(t, f, newTestSSO(f, ProviderMicrosoft, ms), ProviderMicrosoft)
		if err != nil || id.Email != "upn@contoso.com" {
			t.Errorf("id = %+v %v", id, err)
		}
	})
	t.Run("different tenant refused", func(t *testing.T) {
		f := newFakeIdP(t)
		f.set(func(f *fakeIdP) {
			f.extraClaims = map[string]any{"tid": "other", "oid": "o", "preferred_username": "x@y.z"}
		})
		if _, err := completeFlow(t, f, newTestSSO(f, ProviderMicrosoft, ms), ProviderMicrosoft); err == nil || !strings.Contains(err.Error(), "tenant") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("no preferred_username and no verified email", func(t *testing.T) {
		f := newFakeIdP(t)
		f.set(func(f *fakeIdP) { f.extraClaims = map[string]any{"tid": "tenant-1", "oid": "o", "email": "m@x.io"} })
		if _, err := completeFlow(t, f, newTestSSO(f, ProviderMicrosoft, ms), ProviderMicrosoft); err == nil {
			t.Error("want error: no trustworthy email")
		}
	})
}

func TestSSOCompleteRejectsBadTokens(t *testing.T) {
	good := map[string]any{"email": "a@b.c", "email_verified": true}
	tests := []struct {
		name  string
		setup func(f *fakeIdP)
		want  string
	}{
		{"wrong audience", func(f *fakeIdP) { f.audience = "someone-else" }, "verify ID token"},
		{"expired", func(f *fakeIdP) { f.expiresIn = -time.Hour }, "verify ID token"},
		{"signed with another key", func(f *fakeIdP) { f.signWithOther = true }, "verify ID token"},
		{"no id_token", func(f *fakeIdP) { f.omitIDToken = true }, "no ID token"},
		{"token nonce differs", func(f *fakeIdP) { n := "attacker-nonce"; f.nonceOverride = &n }, "nonce"},
		{"token nonce empty", func(f *fakeIdP) { n := ""; f.nonceOverride = &n }, "nonce"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeIdP(t)
			f.set(func(f *fakeIdP) { f.extraClaims = good; tt.setup(f) })
			_, err := completeFlow(t, f, newTestSSO(f, ProviderClerk, nil), ProviderClerk)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want mention of %q", err, tt.want)
			}
		})
	}
}

func TestSSOCompleteNonceMismatchFromCaller(t *testing.T) {
	f := newFakeIdP(t)
	f.set(func(f *fakeIdP) { f.extraClaims = map[string]any{"email": "a@b.c", "email_verified": true} })
	s := newTestSSO(f, ProviderClerk, nil)
	ctx := context.Background()
	start, _ := s.Begin(ctx, ProviderClerk)
	code := f.authorize(start.URL)
	if _, err := s.Complete(ctx, ProviderClerk, code, start.CodeVerifier, "a-different-nonce"); err == nil || !strings.Contains(err.Error(), "nonce") {
		t.Errorf("err = %v", err)
	}
}

func TestSSOCompletePKCEVerifierMismatch(t *testing.T) {
	f := newFakeIdP(t)
	f.set(func(f *fakeIdP) { f.extraClaims = map[string]any{"email": "a@b.c", "email_verified": true} })
	s := newTestSSO(f, ProviderClerk, nil)
	ctx := context.Background()
	start, _ := s.Begin(ctx, ProviderClerk)
	other, _ := s.Begin(ctx, ProviderClerk)
	code := f.authorize(start.URL)
	if _, err := s.Complete(ctx, ProviderClerk, code, other.CodeVerifier, start.Nonce); err == nil || !strings.Contains(err.Error(), "exchange code") {
		t.Errorf("a verifier from another login must be refused, got %v", err)
	}
}

func TestSSOCompleteCodeSingleUseAndUnknown(t *testing.T) {
	f := newFakeIdP(t)
	f.set(func(f *fakeIdP) { f.extraClaims = map[string]any{"email": "a@b.c", "email_verified": true} })
	s := newTestSSO(f, ProviderClerk, nil)
	ctx := context.Background()
	start, _ := s.Begin(ctx, ProviderClerk)
	code := f.authorize(start.URL)
	if _, err := s.Complete(ctx, ProviderClerk, code, start.CodeVerifier, start.Nonce); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Complete(ctx, ProviderClerk, code, start.CodeVerifier, start.Nonce); err == nil {
		t.Error("code replay must fail")
	}
	if _, err := s.Complete(ctx, ProviderClerk, "garbage", start.CodeVerifier, start.Nonce); err == nil {
		t.Error("unknown code must fail")
	}
}

func TestSSOCompleteCanceledContext(t *testing.T) {
	f := newFakeIdP(t)
	s := newTestSSO(f, ProviderClerk, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Begin(ctx, ProviderClerk); err == nil {
		t.Error("canceled context must fail discovery")
	}
}
