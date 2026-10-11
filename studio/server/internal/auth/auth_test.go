package auth

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

func TestKeysRoundTrip(t *testing.T) {
	key, prefix := NewKey(APIKeyPrefix)
	got, ok := SplitKey(APIKeyPrefix, key)
	if !ok || got != prefix || !strings.HasPrefix(key, "csk_"+prefix+"_") {
		t.Fatalf("key %q prefix %q split %q %v", key, prefix, got, ok)
	}
	for _, bad := range []string{"", "csk_", "csk_short_x", "cst_" + key[4:], key[:20]} {
		if _, ok := SplitKey(APIKeyPrefix, bad); ok {
			t.Errorf("%q should not parse", bad)
		}
	}
	if HashToken(key) == HashToken(key+"x") || !TokensEqual(HashToken(key), HashToken(key)) {
		t.Error("hash comparison broken")
	}
	if len(NewUUID()) != 36 || NewUUID() == NewUUID() {
		t.Error("uuid broken")
	}
}

func TestPasswords(t *testing.T) {
	for _, bad := range []string{"short", "            ", strings.Repeat("x", 73)} {
		if _, err := HashPassword(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
	hash, err := HashPassword("a perfectly fine password")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(hash, "a perfectly fine password") || CheckPassword(hash, "wrong password!!") || CheckPassword("", "x") {
		t.Error("password check broken")
	}
}

func TestPrincipal(t *testing.T) {
	p := &Principal{AreaIDs: []string{"a"}, Permissions: map[string]bool{CrewsView: true, AdminAudit: true}}
	if !p.Can(CrewsView) || p.Can(CrewsEdit) || !p.SeesAreas([]string{"b", "a"}) || p.SeesAreas([]string{"b"}) || p.SeesAreas(nil) {
		t.Error("principal checks broken")
	}
	if got := p.PermissionList(); len(got) != 2 || got[0] != CrewsView {
		t.Errorf("list = %v", got)
	}
	all := &Principal{AllAreas: true}
	if !all.SeesAreas(nil) || !all.HasArea("z") {
		t.Error("all areas should see everything")
	}
	var none *Principal
	if none.Can(CrewsView) || none.SeesAreas([]string{"a"}) || none.HasArea("a") {
		t.Error("nil principal must be denied")
	}
	if !Known(AdminUsers) || Known("admin.everything") {
		t.Error("catalog lookup broken")
	}
}

func TestIdentityFromClaims(t *testing.T) {
	yes, no := true, false
	clerk := ClerkProvider("https://x.clerk.accounts.dev", "id", "s")
	ms := MicrosoftProvider("tid", "id", "s")
	if _, err := identityFromClaims(clerk, idClaims{Subject: "u", Email: "a@b.c", EmailVerified: &no}); err != ErrUnverifiedEmail {
		t.Errorf("unverified email = %v", err)
	}
	if _, err := identityFromClaims(clerk, idClaims{Subject: "u", Email: "a@b.c"}); err != ErrUnverifiedEmail {
		t.Errorf("Clerk must say the email is verified, got %v", err)
	}
	if id, err := identityFromClaims(clerk, idClaims{Subject: "u", Email: "A@B.c", EmailVerified: &yes}); err != nil || id.Email != "a@b.c" {
		t.Errorf("clerk = %+v %v", id, err)
	}
	id, err := identityFromClaims(ms, idClaims{Subject: "pairwise", ObjectID: "oid", TenantID: "tid",
		PreferredUsername: "Person@Contoso.com", Email: "attacker@evil.example"})
	if err != nil || id.Subject != "tid:oid" || id.Email != "person@contoso.com" || id.Name != id.Email {
		t.Errorf("an unverified Microsoft email claim must not be used: %+v %v", id, err)
	}
	id, _ = identityFromClaims(ms, idClaims{Subject: "s", ObjectID: "o", TenantID: "tid", PreferredUsername: "upn@contoso.com",
		Email: "Mail@Contoso.com", EmailDomainVerified: &yes})
	if id.Email != "mail@contoso.com" {
		t.Errorf("verified-domain email = %q", id.Email)
	}
	if _, err := identityFromClaims(ms, idClaims{Subject: "s", TenantID: "other", PreferredUsername: "x@y.z"}); err == nil {
		t.Error("another tenant must be refused")
	}
	if _, err := identityFromClaims(clerk, idClaims{Subject: "u", EmailVerified: &yes}); err == nil {
		t.Error("an identity without email must be refused")
	}
	if _, err := identityFromClaims(clerk, idClaims{Email: "a@b.c", EmailVerified: &yes}); err == nil {
		t.Error("an identity without subject must be refused")
	}
}

func TestSSOConfiguration(t *testing.T) {
	s := NewSSO("https://studio.example.com/", MicrosoftProvider("tenant", "id", "secret"), ClerkProvider("https://x.clerk.accounts.dev/", "id", "s"))
	if !s.Has(ProviderMicrosoft) || s.Has("google") || len(s.Providers()) != 2 || s.Providers()[0].Key != ProviderMicrosoft {
		t.Errorf("providers = %+v", s.Providers())
	}
	if got := s.RedirectURL(ProviderClerk); got != "https://studio.example.com/api/auth/clerk/callback" {
		t.Errorf("redirect = %s", got)
	}
	if s.providers[ProviderMicrosoft].cfg.Issuer != "https://login.microsoftonline.com/tenant/v2.0" ||
		s.providers[ProviderClerk].cfg.Issuer != "https://x.clerk.accounts.dev" {
		t.Error("issuers wrong")
	}
}

func TestPasswordPolicyBoundaries(t *testing.T) {
	tests := []struct {
		name string
		pw   string
		ok   bool
	}{
		{"11 chars", strings.Repeat("a", 11), false},
		{"12 chars", strings.Repeat("a", 12), true},
		{"72 bytes", strings.Repeat("a", 72), true},
		{"73 bytes", strings.Repeat("a", 73), false},
		{"empty", "", false},
		{"12 spaces", strings.Repeat(" ", 12), false},
		{"12 tabs", strings.Repeat("\t", 12), false},
		{"12 runes multi-byte", strings.Repeat("é", 12), true},
		{"11 runes multi-byte", strings.Repeat("é", 11), false},
		{"36 two-byte runes = 72 bytes", strings.Repeat("é", 36), true},
		{"37 two-byte runes = 74 bytes", strings.Repeat("é", 37), false},
		{"emoji 12 runes", strings.Repeat("😀", 12), true},
		{"padded but not blank", "           x", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problem := CheckPasswordPolicy(tt.pw)
			if (problem == "") != tt.ok {
				t.Errorf("CheckPasswordPolicy = %q, ok=%v", problem, tt.ok)
			}
			if tt.ok {
				return
			}
			if _, err := HashPassword(tt.pw); err == nil {
				t.Error("HashPassword must refuse")
			}
		})
	}
}

func TestSplitKeyMalformed(t *testing.T) {
	key, _ := NewKey(TriggerPrefix)
	tests := []struct {
		name, kind, key string
	}{
		{"empty", TriggerPrefix, ""},
		{"wrong kind", APIKeyPrefix, key},
		{"no separator after kind", TriggerPrefix, "cstabcdefgh_" + strings.Repeat("x", 40)},
		{"prefix too short", TriggerPrefix, "cst_abc_" + strings.Repeat("x", 40)},
		{"prefix too long", TriggerPrefix, "cst_abcdefghi_" + strings.Repeat("x", 40)},
		{"secret too short", TriggerPrefix, "cst_abcdefgh_" + strings.Repeat("x", 39)},
		{"no secret", TriggerPrefix, "cst_abcdefgh"},
		{"only kind", TriggerPrefix, "cst_"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if p, ok := SplitKey(tt.kind, tt.key); ok {
				t.Errorf("parsed %q", p)
			}
		})
	}
	if p, ok := SplitKey(TriggerPrefix, "cst_abcdefgh_"+strings.Repeat("x", 40)); !ok || p != "abcdefgh" {
		t.Errorf("boundary key rejected: %q %v", p, ok)
	}
}

func TestNewKeyShapeAndUniqueness(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		key, prefix := NewKey(APIKeyPrefix)
		if len(prefix) != 8 || prefix != strings.ToLower(prefix) || strings.ContainsAny(prefix, "-_") {
			t.Fatalf("prefix %q must be 8 lowercase chars without separators", prefix)
		}
		if seen[key] {
			t.Fatal("duplicate key")
		}
		seen[key] = true
		if _, ok := SplitKey(APIKeyPrefix, key); !ok {
			t.Fatalf("generated key %q does not split", key)
		}
	}
}

func TestTokensEqual(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"abc", "abc", true},
		{"abc", "abd", false},
		{"abc", "abcd", false},
		{"", "", true},
		{"", "a", false},
	}
	for _, tt := range tests {
		if got := TokensEqual(tt.a, tt.b); got != tt.want {
			t.Errorf("TokensEqual(%q,%q) = %v", tt.a, tt.b, got)
		}
	}
}

func TestRandomTokenAndHash(t *testing.T) {
	a, b := RandomToken(32), RandomToken(32)
	if a == b || len(a) != 43 {
		t.Errorf("tokens %q %q", a, b)
	}
	if len(RandomToken(0)) != 0 {
		t.Error("zero bytes -> empty token")
	}
	h := HashToken("x")
	if len(h) != 64 || h != HashToken("x") || h == HashToken("y") {
		t.Errorf("hash = %q", h)
	}
}

func TestNewUUIDFormat(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	for i := 0; i < 100; i++ {
		if id := NewUUID(); !re.MatchString(id) {
			t.Fatalf("not a v4 uuid: %s", id)
		}
	}
}

func TestPrincipalContext(t *testing.T) {
	if FromContext(context.Background()) != nil {
		t.Error("anonymous context must yield nil")
	}
	p := &Principal{UserID: "u"}
	if got := FromContext(WithPrincipal(context.Background(), p)); got != p {
		t.Error("principal not round-tripped")
	}
}

func TestPrincipalAreaRules(t *testing.T) {
	p := &Principal{AreaIDs: []string{"a", "b"}}
	tests := []struct {
		name  string
		areas []string
		sees  bool
	}{
		{"nil areas", nil, false},
		{"empty areas", []string{}, false},
		{"one shared", []string{"x", "b"}, true},
		{"none shared", []string{"x", "y"}, false},
	}
	for _, tt := range tests {
		if got := p.SeesAreas(tt.areas); got != tt.sees {
			t.Errorf("%s: SeesAreas = %v", tt.name, got)
		}
	}
	if !p.HasArea("a") || p.HasArea("z") || p.HasArea("") {
		t.Error("HasArea broken")
	}
	// a principal with no areas at all sees only nothing
	none := &Principal{}
	if none.SeesAreas([]string{"a"}) || none.HasArea("a") {
		t.Error("principal without areas must see nothing")
	}
}

func TestPermissionListOrderAndUnknown(t *testing.T) {
	p := &Principal{Permissions: map[string]bool{AdminAudit: true, OverviewView: true, "bogus": true, RunsView: false}}
	got := p.PermissionList()
	if len(got) != 2 || got[0] != OverviewView || got[1] != AdminAudit {
		t.Errorf("list = %v", got)
	}
	if empty := (&Principal{}).PermissionList(); empty == nil || len(empty) != 0 {
		t.Errorf("empty list must be non-nil, got %#v", empty)
	}
}

func TestCatalogHasNoDuplicates(t *testing.T) {
	seen := map[string]bool{}
	for _, k := range Catalog {
		if seen[k] {
			t.Errorf("duplicate permission %s", k)
		}
		seen[k] = true
		if !Known(k) {
			t.Errorf("%s not Known", k)
		}
	}
	if Known("") {
		t.Error("empty key is not a permission")
	}
}

func TestProviderConstructors(t *testing.T) {
	ms := MicrosoftProvider("t", "c", "s")
	if ms.TenantID != "t" || ms.AuthParams["prompt"] != "select_account" || ms.Key != ProviderMicrosoft {
		t.Errorf("%+v", ms)
	}
	cl := ClerkProvider("https://c.example.com///", "c", "s")
	if cl.Issuer != "https://c.example.com" || cl.Key != ProviderClerk {
		t.Errorf("%+v", cl)
	}
}

func TestIdentityFromClaimsEdgeCases(t *testing.T) {
	yes := true
	clerk := ClerkProvider("https://c", "id", "s")
	ms := MicrosoftProvider("tid", "id", "s")
	tests := []struct {
		name string
		cfg  ProviderConfig
		c    idClaims
		ok   bool
		sub  string
	}{
		{"email without @", clerk, idClaims{Subject: "s", Email: "nope", EmailVerified: &yes}, false, ""},
		{"whitespace-padded email", clerk, idClaims{Subject: "s", Email: "  A@B.c ", EmailVerified: &yes}, true, "s"},
		{"microsoft missing oid keeps sub", ms, idClaims{Subject: "pw", TenantID: "tid", PreferredUsername: "a@b.c"}, true, "pw"},
		{"microsoft missing tid with tenant configured", ms, idClaims{Subject: "pw", ObjectID: "o", PreferredUsername: "a@b.c"}, false, ""},
		{"microsoft no tenant configured accepts any", ProviderConfig{Key: ProviderMicrosoft}, idClaims{Subject: "s", TenantID: "x", ObjectID: "o", PreferredUsername: "a@b.c"}, true, "x:o"},
		{"microsoft preferred_username not an email", ms, idClaims{Subject: "s", TenantID: "tid", PreferredUsername: "plainname"}, false, ""},
		{"microsoft subject empty", ms, idClaims{TenantID: "tid", PreferredUsername: "a@b.c"}, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := identityFromClaims(tt.cfg, tt.c)
			if (err == nil) != tt.ok {
				t.Fatalf("err = %v", err)
			}
			if tt.ok && id.Subject != tt.sub {
				t.Errorf("subject = %q", id.Subject)
			}
			if tt.ok && id.Email != strings.ToLower(strings.TrimSpace(id.Email)) {
				t.Errorf("email not normalised: %q", id.Email)
			}
		})
	}
}
