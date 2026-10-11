package api

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"crewstudio/internal/auth"
	"crewstudio/internal/store"
)

const loginStateTTL = 10 * time.Minute

func (s *Server) authRoutes(r chi.Router) {
	r.Route("/auth", func(r chi.Router) {
		r.Use(middleware.Timeout(requestTimeout))
		r.Get("/status", s.authStatus)
		r.Get("/me", s.me)
		r.Post("/setup", s.setup)
		r.Post("/login", s.localLogin)
		r.Post("/logout", s.logout)
		r.Get("/{provider}/login", s.ssoLogin)
		r.Get("/{provider}/callback", s.ssoCallback)
	})
}

type authStatus struct {
	SetupRequired bool                `json:"setup_required"`
	LocalLogin    bool                `json:"local_login"`
	Providers     []auth.ProviderInfo `json:"providers"`
}

func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.HumanUserCount(r.Context())
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	providers := []auth.ProviderInfo{}
	if s.sso != nil {
		providers = s.sso.Providers()
	}
	ok(w, authStatus{SetupRequired: n == 0, LocalLogin: s.opts.LocalLogin, Providers: providers})
}

type meResponse struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Name        string   `json:"name"`
	Email       string   `json:"email"`
	AllAreas    bool     `json:"all_areas"`
	AreaIDs     []string `json:"area_ids"`
	Permissions []string `json:"permissions"`
	Via         string   `json:"via"`
}

func meOf(p *auth.Principal) meResponse {
	return meResponse{ID: p.UserID, Kind: p.Kind, Name: p.Name, Email: p.Email, AllAreas: p.AllAreas,
		AreaIDs: orEmpty(p.AreaIDs), Permissions: p.PermissionList(), Via: p.Via}
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p == nil {
		fail(w, http.StatusUnauthorized, "unauthenticated", "Sign in to continue.")
		return
	}
	ok(w, meOf(p))
}

func (s *Server) setCookie(w http.ResponseWriter, name, value, path string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: path, MaxAge: int(maxAge.Seconds()),
		HttpOnly: true, Secure: s.secureCookies(), SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearCookie(w http.ResponseWriter, name, path string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: path, MaxAge: -1, HttpOnly: true,
		Secure: s.secureCookies(), SameSite: http.SameSiteLaxMode})
}

// signIn starts a session for userID and answers with the signed-in person.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID, provider string) (*auth.Principal, error) {
	p, err := s.store.LoadPrincipal(r.Context(), userID)
	if err != nil {
		return nil, err
	}
	token, err := s.store.CreateSession(r.Context(), userID, provider, clientIP(r), r.UserAgent())
	if err != nil {
		return nil, err
	}
	if err := s.store.RecordLoginSuccess(r.Context(), userID); err != nil {
		s.log.Warn("record sign-in", "err", err)
	}
	s.setCookie(w, sessionCookie, token, "/", store.SessionLifetime)
	s.audit(r, "login", p.UserID, p.Name, map[string]any{"provider": provider})
	p.Via = auth.ViaSession
	return p, nil
}

func (s *Server) audit(r *http.Request, action, actorID, actorName string, details map[string]any) {
	if err := s.store.AppendAudit(r.Context(), store.AuditEntry{
		Action: action, ActorID: actorID, ActorName: actorName, IP: clientIP(r), Details: details,
	}); err != nil {
		s.log.Warn("append audit event", "action", action, "err", err)
	}
}

type setupRequest struct {
	Token    string `json:"token"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	var req setupRequest
	if !decode(w, r, &req) {
		return
	}
	if !s.logins.allow("setup:" + clientIP(r)) {
		tooMany(w, "Too many attempts; wait a minute.")
		return
	}
	s.setupMu.Lock()
	expected := s.setupToken
	if time.Now().After(s.setupExpires) {
		expected = ""
	}
	s.setupMu.Unlock()
	if expected == "" || !auth.TokensEqual(auth.HashToken(req.Token), auth.HashToken(expected)) {
		fail(w, http.StatusForbidden, "bad_setup_token", "That setup token is not valid. Use the link printed in the service log.")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	name := strings.TrimSpace(req.Name)
	var details []string
	if !strings.Contains(email, "@") || len(email) > 254 {
		details = append(details, "email: must be a valid email address")
	}
	if name == "" {
		name = email
	}
	if problem := auth.CheckPasswordPolicy(req.Password); problem != "" {
		details = append(details, problem)
	}
	if len(details) > 0 {
		failErr(w, r, s.log, invalid(details...))
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	id, err := s.store.CreateFirstAdmin(r.Context(), email, name, hash)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	s.setupMu.Lock()
	s.setupToken = ""
	s.setupMu.Unlock()
	s.audit(r, "setup", id, name, nil)
	p, err := s.startSession(w, r, id, auth.ProviderLocal)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	created(w, meOf(p))
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) localLogin(w http.ResponseWriter, r *http.Request) {
	if !s.opts.LocalLogin {
		fail(w, http.StatusNotFound, "not_found", "Password sign-in is turned off; use single sign-on.")
		return
	}
	var req loginRequest
	if !decode(w, r, &req) {
		return
	}
	if !s.logins.allow("login:" + clientIP(r)) {
		tooMany(w, "Too many sign-in attempts; wait a minute.")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	badCredentials := func(userID string) {
		s.audit(r, "login_failed", userID, email, map[string]any{"provider": auth.ProviderLocal})
		fail(w, http.StatusUnauthorized, "invalid_credentials", "That email and password do not match an administrator account.")
	}
	acct, err := s.store.FindLocalLogin(r.Context(), email)
	if errors.Is(err, store.ErrNotFound) {
		auth.CheckPassword("", req.Password) // spend the same time as a real check
		badCredentials("")
		return
	}
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	// The password is checked first, so a locked account looks like any other to someone without it
	if !auth.CheckPassword(acct.PasswordHash, req.Password) {
		if acct.PasswordHash != "" {
			if err := s.store.RecordLoginFailure(r.Context(), acct.UserID); err != nil {
				s.log.Warn("record failed sign-in", "err", err)
			}
		}
		badCredentials(acct.UserID)
		return
	}
	if acct.LockedUntil != nil && acct.LockedUntil.After(time.Now()) {
		fail(w, http.StatusLocked, "locked", "Too many wrong passwords. The account is locked for a few minutes; you can still sign in with SSO.")
		return
	}
	p, err := s.store.LoadPrincipal(r.Context(), acct.UserID)
	if err != nil || !acct.Active || !p.Can(auth.AdminUsers) {
		// Password sign-in is only for administrators; everyone else uses SSO
		badCredentials(acct.UserID)
		return
	}
	p, err = s.startSession(w, r, acct.UserID, auth.ProviderLocal)
	if err != nil {
		failErr(w, r, s.log, err)
		return
	}
	ok(w, meOf(p))
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if p := principal(r); p != nil && p.SessionHash != "" {
		if err := s.store.DeleteSession(r.Context(), p.SessionHash); err != nil {
			failErr(w, r, s.log, err)
			return
		}
		s.audit(r, "logout", p.UserID, p.Name, nil)
	}
	s.clearCookie(w, sessionCookie, "/")
	noContent(w)
}

// safeReturnTo keeps redirects on this site: a path, never a scheme-relative or absolute URL.
func safeReturnTo(raw string) string {
	if raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.HasPrefix(raw, "/\\") ||
		strings.ContainsAny(raw, "\r\n") || strings.HasPrefix(raw, "/api/") {
		return "/"
	}
	return raw
}

func (s *Server) loginRedirect(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, strings.TrimRight(s.opts.PublicURL, "/")+"/login?error="+url.QueryEscape(code), http.StatusFound)
}

func (s *Server) ssoLogin(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	if s.sso == nil || !s.sso.Has(provider) {
		fail(w, http.StatusNotFound, "not_found", "That sign-in provider is not configured.")
		return
	}
	// The callback goes to the public URL, so the sign-in must start there too (its cookie is per host)
	if public, err := url.Parse(s.opts.PublicURL); err == nil && public.Host != "" && !strings.EqualFold(public.Host, r.Host) {
		http.Redirect(w, r, strings.TrimRight(s.opts.PublicURL, "/")+r.URL.RequestURI(), http.StatusFound)
		return
	}
	if !s.logins.allow("sso:" + clientIP(r)) {
		tooMany(w, "Too many sign-in attempts; wait a minute.")
		return
	}
	start, err := s.sso.Begin(r.Context(), provider)
	if err != nil {
		s.log.Error("start sso sign-in", "provider", provider, "err", err)
		s.loginRedirect(w, r, "provider_error")
		return
	}
	if err := s.store.SaveLoginState(r.Context(), start.State, store.LoginState{
		Provider: provider, Nonce: start.Nonce, CodeVerifier: start.CodeVerifier,
		ReturnTo: safeReturnTo(r.URL.Query().Get("return_to")),
	}); err != nil {
		failErr(w, r, s.log, err)
		return
	}
	// Binds the sign-in to this browser, so a sign-in started elsewhere cannot be completed here
	s.setCookie(w, loginCookie, start.State, "/api/auth/", loginStateTTL)
	http.Redirect(w, r, start.URL, http.StatusFound)
}

func (s *Server) ssoCallback(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	q := r.URL.Query()
	s.clearCookie(w, loginCookie, "/api/auth/")
	if s.sso == nil || !s.sso.Has(provider) {
		s.loginRedirect(w, r, "provider_error")
		return
	}
	state := q.Get("state")
	cookie, err := r.Cookie(loginCookie)
	if state == "" || err != nil || !auth.TokensEqual(auth.HashToken(cookie.Value), auth.HashToken(state)) {
		s.loginRedirect(w, r, "expired")
		return
	}
	pending, err := s.store.TakeLoginState(r.Context(), state)
	if err != nil || pending.Provider != provider {
		s.loginRedirect(w, r, "expired")
		return
	}
	if q.Get("error") != "" || q.Get("code") == "" {
		s.log.Warn("sso provider returned an error", "provider", provider, "error", q.Get("error"))
		s.loginRedirect(w, r, "provider_error")
		return
	}
	identity, err := s.sso.Complete(r.Context(), provider, q.Get("code"), pending.CodeVerifier, pending.Nonce)
	if err != nil {
		s.log.Warn("sso sign-in failed", "provider", provider, "err", err)
		code := "provider_error"
		if errors.Is(err, auth.ErrUnverifiedEmail) {
			code = "unverified_email"
		}
		s.audit(r, "login_failed", "", "", map[string]any{"provider": provider, "reason": code})
		s.loginRedirect(w, r, code)
		return
	}
	userID, err := s.store.ResolveSSOUser(r.Context(), identity)
	if err != nil {
		code := "provider_error"
		if errors.Is(err, store.ErrNoAccount) {
			code = "not_invited"
		} else {
			s.log.Error("resolve sso user", "err", err)
		}
		s.audit(r, "login_failed", "", identity.Email, map[string]any{"provider": provider, "reason": code})
		s.loginRedirect(w, r, code)
		return
	}
	if _, err := s.startSession(w, r, userID, provider); err != nil {
		code := "provider_error"
		if errors.Is(err, store.ErrNotFound) {
			code = "disabled"
		}
		s.audit(r, "login_failed", userID, identity.Email, map[string]any{"provider": provider, "reason": code})
		s.loginRedirect(w, r, code)
		return
	}
	http.Redirect(w, r, strings.TrimRight(s.opts.PublicURL, "/")+pending.ReturnTo, http.StatusFound)
}
