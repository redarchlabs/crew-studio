package api

import (
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// uploadHeader must accompany multipart uploads. A custom header cannot be sent cross-site without a CORS
// preflight, which checkOrigin refuses, so multipart forms posted by other sites are rejected.
const uploadHeader = "X-Requested-With"

// requireJSON rejects POST/PUT/PATCH requests that are not application/json (or a multipart upload with the
// upload header). Browsers cannot send either cross-site without a CORS preflight, so this blocks form-based CSRF.
func requireJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch:
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			upload := mediaType == "multipart/form-data" && r.Header.Get(uploadHeader) == "crewstudio"
			if err != nil || (mediaType != "application/json" && !upload) {
				fail(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Send the request body as application/json.")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// HostsFromOrigins returns the host names of the allowed origins, for the Host allow-list.
func HostsFromOrigins(origins []string) []string {
	var hosts []string
	for _, origin := range origins {
		if u, err := url.Parse(origin); err == nil && u.Hostname() != "" {
			hosts = append(hosts, strings.ToLower(u.Hostname()))
		}
	}
	return hosts
}

// checkHost refuses requests whose Host header is not this machine (or an explicitly allowed name).
// Without it a web page could use DNS rebinding to point its own domain at 127.0.0.1 and call the API
// as if it were same-origin.
func checkHost(allowed []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := r.Host
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			}
			host = strings.ToLower(strings.Trim(host, "[]"))
			if host == "localhost" || slices.Contains(allowed, host) {
				next.ServeHTTP(w, r)
				return
			}
			if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
				next.ServeHTTP(w, r)
				return
			}
			fail(w, http.StatusMisdirectedRequest, "forbidden_host", "This host name is not allowed.")
		})
	}
}

// checkOrigin refuses browser requests from origins that are neither this server nor allow-listed.
// Requests without an Origin header (curl, same-origin GETs) pass. Matching r.Host is safe because
// checkHost has already rejected foreign host names.
func checkOrigin(allowed []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" || origin == "http://"+r.Host || origin == "https://"+r.Host {
				next.ServeHTTP(w, r)
				return
			}
			if !slices.Contains(allowed, origin) {
				fail(w, http.StatusForbidden, "forbidden_origin", "Requests from this origin are not allowed.")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Last-Event-ID, Authorization, Idempotency-Key, X-Requested-With")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		// React Flow positions nodes with inline styles, so styles allow 'unsafe-inline'; scripts do not
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
			"img-src 'self' data:; font-src 'self' data:; connect-src 'self'; object-src 'none'; "+
			"frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			level := slog.LevelDebug
			if ww.Status() >= http.StatusInternalServerError {
				level = slog.LevelError
			} else if ww.Status() >= http.StatusBadRequest {
				level = slog.LevelWarn
			}
			log.Log(r.Context(), level, "http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(),
				"duration_ms", time.Since(start).Milliseconds())
		})
	}
}
