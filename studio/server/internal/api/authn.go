package api

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"crewstudio/internal/auth"
	"crewstudio/internal/store"
)

const (
	sessionCookie = "crewstudio_session"
	loginCookie   = "crewstudio_login"
)

// authenticate resolves the caller from an API key or session cookie. It never rejects: anonymous requests
// continue without a principal, and requireSignedIn turns them away where needed.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p *auth.Principal
		var err error
		if header := r.Header.Get("Authorization"); header != "" {
			key, found := strings.CutPrefix(header, "Bearer ")
			if !found || !strings.HasPrefix(key, auth.APIKeyPrefix+"_") {
				// Trigger tokens and other schemes are handled by their own endpoints
				next.ServeHTTP(w, r)
				return
			}
			key = strings.TrimSpace(key)
			if p, err = s.store.APIKeyPrincipal(r.Context(), key); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					s.auditRejectedKey(r, key)
				} else {
					s.log.Error("check api key", "err", err)
				}
				fail(w, http.StatusUnauthorized, "unauthenticated", "That API key is not valid, has expired, or was revoked.")
				return
			}
		} else if c, cerr := r.Cookie(sessionCookie); cerr == nil && c.Value != "" {
			p, err = s.store.SessionPrincipal(r.Context(), c.Value)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				s.log.Error("check session", "err", err)
			}
		}
		if p == nil {
			next.ServeHTTP(w, r)
			return
		}
		ctx := auth.WithPrincipal(r.Context(), p)
		ctx = store.WithActor(ctx, store.Actor{ID: p.UserID, Name: p.Name, APIKeyID: p.APIKeyID})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// rejectedKeyAuditEvery is how often the same refused key (by prefix) from the same address is audited again.
const rejectedKeyAuditEvery = 10 * time.Minute

// auditRejectedKey records a refused API key by its prefix (never the secret), so admins can spot a leaked or
// guessed key being tried. Repeats from the same address within rejectedKeyAuditEvery are not recorded again.
func (s *Server) auditRejectedKey(r *http.Request, key string) {
	prefix, shaped := auth.SplitKey(auth.APIKeyPrefix, key)
	if !s.keyRejections.allow(prefix + "|" + clientIP(r)) {
		return
	}
	details := map[string]any{"path": r.URL.Path}
	if shaped {
		details["prefix"] = prefix
	}
	s.audit(r, "api_key_rejected", "", "", details)
}

func (s *Server) requireSignedIn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth.FromContext(r.Context()) == nil {
			fail(w, http.StatusUnauthorized, "unauthenticated", "Sign in to continue.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// need allows the request only when the principal holds every permission.
func (s *Server) need(perms ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := auth.FromContext(r.Context())
			for _, perm := range perms {
				if !p.Can(perm) {
					forbidden(w, perm)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// needAny allows the request when the principal holds at least one of the permissions.
func (s *Server) needAny(perms ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := auth.FromContext(r.Context())
			for _, perm := range perms {
				if p.Can(perm) {
					next.ServeHTTP(w, r)
					return
				}
			}
			forbidden(w, perms...)
		})
	}
}

func forbidden(w http.ResponseWriter, perms ...string) {
	details := make([]string, len(perms))
	for i, p := range perms {
		details[i] = "needs: " + p
	}
	fail(w, http.StatusForbidden, "forbidden", "You do not have permission to do that.", details...)
}

func principal(r *http.Request) *auth.Principal { return auth.FromContext(r.Context()) }

func scopeOf(p *auth.Principal) store.Scope {
	if p == nil {
		return store.Scope{}
	}
	return store.Scope{All: p.AllAreas, AreaIDs: p.AreaIDs}
}

// clientIP is the connecting address. Crew Studio does not trust X-Forwarded-For: behind a reverse proxy
// every request shares the proxy's address, which only makes limits stricter.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// limitByIP applies a token bucket per connecting address, before any credential is checked.
func (s *Server) limitByIP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.byIP.allow(clientIP(r)) {
			tooMany(w, "Too many requests from this address; slow down and try again.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// rateLimit applies a token bucket per principal (or per IP when signed out).
func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := "ip:" + clientIP(r)
		if p := principal(r); p != nil {
			key = "user:" + p.UserID
		}
		if !s.requests.allow(key) {
			tooMany(w, "Too many requests; slow down and try again.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func tooMany(w http.ResponseWriter, msg string) {
	w.Header().Set("Retry-After", "5")
	fail(w, http.StatusTooManyRequests, "rate_limited", msg)
}

// keyedLimiter keeps one token bucket per key and forgets idle keys.
type keyedLimiter struct {
	mu       sync.Mutex
	limit    rate.Limit
	burst    int
	buckets  map[string]*bucket
	lastSwep time.Time
}

type bucket struct {
	limiter *rate.Limiter
	seen    time.Time
}

const limiterIdle = 10 * time.Minute

func newKeyedLimiter(limit rate.Limit, burst int) *keyedLimiter {
	return &keyedLimiter{limit: limit, burst: burst, buckets: map[string]*bucket{}, lastSwep: time.Now()}
}

func (k *keyedLimiter) allow(key string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	now := time.Now()
	if now.Sub(k.lastSwep) > limiterIdle {
		for id, b := range k.buckets {
			if now.Sub(b.seen) > limiterIdle {
				delete(k.buckets, id)
			}
		}
		k.lastSwep = now
	}
	b, ok := k.buckets[key]
	if !ok {
		b = &bucket{limiter: rate.NewLimiter(k.limit, k.burst)}
		k.buckets[key] = b
	}
	b.seen = now
	return b.limiter.Allow()
}

// counter bounds how many of something each key holds at once.
type counter struct {
	mu     sync.Mutex
	counts map[string]int
	max    int
}

func (c *counter) acquire(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.counts[key] >= c.max {
		return false
	}
	c.counts[key]++
	return true
}

func (c *counter) release(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.counts[key]--; c.counts[key] <= 0 {
		delete(c.counts, key)
	}
}
