package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	ProviderClerk     = "clerk"
	ProviderMicrosoft = "microsoft"
	ProviderLocal     = "local"

	ssoHTTPTimeout = 15 * time.Second
)

// ProviderConfig describes one OpenID Connect provider.
type ProviderConfig struct {
	Key          string
	Label        string
	Issuer       string
	ClientID     string
	ClientSecret string
	// Extra authorization parameters, e.g. prompt=select_account
	AuthParams map[string]string
	// TenantID, for Microsoft, is the only tenant whose accounts may sign in
	TenantID string
}

// ClerkProvider builds the Clerk configuration. issuer is the Clerk Frontend API URL.
func ClerkProvider(issuer, clientID, clientSecret string) ProviderConfig {
	return ProviderConfig{
		Key: ProviderClerk, Label: "Clerk", Issuer: strings.TrimRight(issuer, "/"),
		ClientID: clientID, ClientSecret: clientSecret,
	}
}

// MicrosoftProvider builds the Microsoft Entra ID configuration for one tenant. Only accounts in that
// tenant can sign in, because the issuer (and so every accepted ID token) names the tenant.
func MicrosoftProvider(tenantID, clientID, clientSecret string) ProviderConfig {
	return ProviderConfig{
		Key: ProviderMicrosoft, Label: "Microsoft", Issuer: "https://login.microsoftonline.com/" + tenantID + "/v2.0",
		ClientID: clientID, ClientSecret: clientSecret,
		AuthParams: map[string]string{"prompt": "select_account"},
		TenantID:   tenantID,
	}
}

// Identity is what a provider vouches for after a successful sign-in.
type Identity struct {
	Provider string
	Subject  string
	Email    string
	Name     string
}

// LoginStart is everything the caller must keep (server side) to finish the sign-in.
type LoginStart struct {
	URL          string
	State        string
	Nonce        string
	CodeVerifier string
}

// SSO runs the OpenID Connect authorization-code flow (with PKCE and nonce) for the configured providers.
type SSO struct {
	publicURL string
	client    *http.Client
	providers map[string]*ssoProvider
	order     []string
}

type ssoProvider struct {
	cfg ProviderConfig

	mu       sync.Mutex
	provider *oidc.Provider
}

// NewSSO prepares the providers. Discovery happens on first use, so a provider being briefly unreachable
// does not stop the service from starting.
func NewSSO(publicURL string, configs ...ProviderConfig) *SSO {
	s := &SSO{
		publicURL: strings.TrimRight(publicURL, "/"),
		client:    &http.Client{Timeout: ssoHTTPTimeout},
		providers: map[string]*ssoProvider{},
	}
	for _, cfg := range configs {
		s.providers[cfg.Key] = &ssoProvider{cfg: cfg}
		s.order = append(s.order, cfg.Key)
	}
	return s
}

// ProviderInfo is a provider as shown on the sign-in page.
type ProviderInfo struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// Providers lists the configured providers in a stable order.
func (s *SSO) Providers() []ProviderInfo {
	out := []ProviderInfo{}
	for _, key := range s.order {
		out = append(out, ProviderInfo{Key: key, Label: s.providers[key].cfg.Label})
	}
	return out
}

// Has reports whether the provider is configured.
func (s *SSO) Has(key string) bool {
	_, ok := s.providers[key]
	return ok
}

// RedirectURL is the callback address registered with the provider.
func (s *SSO) RedirectURL(key string) string {
	return s.publicURL + "/api/auth/" + key + "/callback"
}

func (s *SSO) discover(ctx context.Context, key string) (*ssoProvider, *oidc.Provider, error) {
	p, ok := s.providers[key]
	if !ok {
		return nil, nil, fmt.Errorf("sign-in provider %q is not configured", key)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.provider == nil {
		provider, err := oidc.NewProvider(oidc.ClientContext(ctx, s.client), p.cfg.Issuer)
		if err != nil {
			return nil, nil, fmt.Errorf("discover %s: %w", p.cfg.Label, err)
		}
		p.provider = provider
	}
	return p, p.provider, nil
}

func (s *SSO) oauthConfig(p *ssoProvider, provider *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     p.cfg.ClientID,
		ClientSecret: p.cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  s.RedirectURL(p.cfg.Key),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}
}

// Begin returns the provider's authorization URL and the values to keep for Complete.
func (s *SSO) Begin(ctx context.Context, key string) (LoginStart, error) {
	p, provider, err := s.discover(ctx, key)
	if err != nil {
		return LoginStart{}, err
	}
	start := LoginStart{State: RandomToken(32), Nonce: RandomToken(32), CodeVerifier: oauth2.GenerateVerifier()}
	opts := []oauth2.AuthCodeOption{oidc.Nonce(start.Nonce), oauth2.S256ChallengeOption(start.CodeVerifier)}
	for k, v := range p.cfg.AuthParams {
		opts = append(opts, oauth2.SetAuthURLParam(k, v))
	}
	start.URL = s.oauthConfig(p, provider).AuthCodeURL(start.State, opts...)
	return start, nil
}

// ErrUnverifiedEmail means the provider says the account's email address is not verified.
var ErrUnverifiedEmail = errors.New("the email address on this account is not verified")

type idClaims struct {
	Subject           string `json:"sub"`
	Email             string `json:"email"`
	EmailVerified     *bool  `json:"email_verified"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
	ObjectID          string `json:"oid"`
	TenantID          string `json:"tid"`
	// Microsoft sets xms_edov when the email's domain is verified in the tenant (an optional claim)
	EmailDomainVerified *bool `json:"xms_edov"`
}

// Complete exchanges the authorization code, verifies the ID token (signature, issuer, audience, expiry,
// nonce) and returns the identity.
func (s *SSO) Complete(ctx context.Context, key, code, codeVerifier, nonce string) (Identity, error) {
	p, provider, err := s.discover(ctx, key)
	if err != nil {
		return Identity{}, err
	}
	ctx = oidc.ClientContext(ctx, s.client)
	cfg := s.oauthConfig(p, provider)
	token, err := cfg.Exchange(ctx, code, oauth2.VerifierOption(codeVerifier))
	if err != nil {
		return Identity{}, fmt.Errorf("exchange code: %w", err)
	}
	rawID, ok := token.Extra("id_token").(string)
	if !ok || rawID == "" {
		return Identity{}, errors.New("the provider returned no ID token")
	}
	idToken, err := provider.Verifier(&oidc.Config{ClientID: p.cfg.ClientID}).Verify(ctx, rawID)
	if err != nil {
		return Identity{}, fmt.Errorf("verify ID token: %w", err)
	}
	if !TokensEqual(idToken.Nonce, nonce) {
		return Identity{}, errors.New("the ID token nonce does not match this sign-in")
	}
	var claims idClaims
	if err := idToken.Claims(&claims); err != nil {
		return Identity{}, fmt.Errorf("read ID token claims: %w", err)
	}
	if key == ProviderClerk && (claims.Email == "" || claims.EmailVerified == nil) {
		// Clerk may leave email (or its verification) out of the ID token; the userinfo endpoint has them
		if info, err := provider.UserInfo(ctx, oauth2.StaticTokenSource(token)); err == nil {
			var extra idClaims
			if info.Claims(&extra) == nil && extra.Subject == claims.Subject {
				if claims.Email == "" {
					claims.Email = extra.Email
				}
				if claims.EmailVerified == nil {
					claims.EmailVerified = extra.EmailVerified
				}
			}
		}
	}
	return identityFromClaims(p.cfg, claims)
}

// identityFromClaims decides which email Crew Studio may trust, because accounts are linked by email on
// first sign-in. Clerk must say the address is verified. Microsoft accounts must come from the configured
// tenant; their email claim is used only when Microsoft marks its domain verified (xms_edov), otherwise the
// user principal name, which only that tenant's administrators can set and must use one of its verified domains.
func identityFromClaims(cfg ProviderConfig, c idClaims) (Identity, error) {
	key := cfg.Key
	id := Identity{Provider: key, Subject: c.Subject, Name: strings.TrimSpace(c.Name)}
	switch key {
	case ProviderMicrosoft:
		if cfg.TenantID != "" && !strings.EqualFold(c.TenantID, cfg.TenantID) {
			return Identity{}, errors.New("the account belongs to a different Microsoft tenant")
		}
		// sub is unique per application; tenant + object id is the stable account id
		if c.ObjectID != "" && c.TenantID != "" {
			id.Subject = c.TenantID + ":" + c.ObjectID
		}
		id.Email = strings.TrimSpace(c.PreferredUsername)
		if c.EmailDomainVerified != nil && *c.EmailDomainVerified && c.Email != "" {
			id.Email = strings.TrimSpace(c.Email)
		}
	default:
		if c.EmailVerified == nil || !*c.EmailVerified {
			return Identity{}, ErrUnverifiedEmail
		}
		id.Email = strings.TrimSpace(c.Email)
	}
	if id.Subject == "" {
		return Identity{}, errors.New("the ID token has no subject")
	}
	if !strings.Contains(id.Email, "@") {
		return Identity{}, errors.New("the provider did not share an email address; allow the email scope")
	}
	id.Email = strings.ToLower(id.Email)
	if id.Name == "" {
		id.Name = id.Email
	}
	return id, nil
}
