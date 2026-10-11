package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
)

const (
	// SessionLifetime is the longest a sign-in lasts; SessionIdle ends it sooner when unused
	SessionLifetime = 7 * 24 * time.Hour
	SessionIdle     = 12 * time.Hour
	// last_seen_at is refreshed at most this often, to keep reads cheap
	sessionTouchEvery = time.Minute
	loginStateTTL     = 10 * time.Minute
	maxUserAgentLen   = 300
)

// CreateSession starts a sign-in and returns the cookie token (only its hash is stored).
func (s *Store) CreateSession(ctx context.Context, userID, provider, ip, userAgent string) (string, error) {
	token := auth.RandomToken(32)
	if len(userAgent) > maxUserAgentLen {
		userAgent = userAgent[:maxUserAgentLen]
	}
	userAgent = strings.ToValidUTF8(strings.ReplaceAll(userAgent, "\x00", ""), "")
	_, err := s.pool.Exec(ctx,
		`INSERT INTO sessions (token_hash, user_id, provider, ip, user_agent, expires_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		auth.HashToken(token), userID, provider, ip, userAgent, time.Now().Add(SessionLifetime))
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return token, nil
}

// SessionPrincipal resolves a session cookie token. Expired, idle or unknown sessions return ErrNotFound.
func (s *Store) SessionPrincipal(ctx context.Context, token string) (*auth.Principal, error) {
	hash := auth.HashToken(token)
	var userID string
	var lastSeen time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, last_seen_at FROM sessions WHERE token_hash = $1 AND expires_at > now() AND last_seen_at > $2`,
		hash, time.Now().Add(-SessionIdle)).Scan(&userID, &lastSeen)
	if err != nil {
		return nil, mapErr(err, "session", "")
	}
	if time.Since(lastSeen) > sessionTouchEvery {
		if _, err := s.pool.Exec(ctx, `UPDATE sessions SET last_seen_at = now() WHERE token_hash = $1`, hash); err != nil {
			return nil, fmt.Errorf("touch session: %w", err)
		}
	}
	p, err := s.LoadPrincipal(ctx, userID)
	if err != nil {
		return nil, err
	}
	p.Via = auth.ViaSession
	p.SessionHash = hash
	return p, nil
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteUserSessions signs a user out everywhere and returns how many sessions ended.
func (s *Store) DeleteUserSessions(ctx context.Context, userID string) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	if err != nil {
		return 0, fmt.Errorf("delete sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}

// PurgeExpired removes finished sessions and abandoned sign-ins.
func (s *Store) PurgeExpired(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now() OR last_seen_at < $1`,
		time.Now().Add(-SessionIdle)); err != nil {
		return fmt.Errorf("purge sessions: %w", err)
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM login_states WHERE expires_at < now()`); err != nil {
		return fmt.Errorf("purge login states: %w", err)
	}
	return nil
}

// LoginState is an SSO sign-in in progress.
type LoginState struct {
	Provider     string
	Nonce        string
	CodeVerifier string
	ReturnTo     string
}

func (s *Store) SaveLoginState(ctx context.Context, state string, ls LoginState) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO login_states (state_hash, provider, nonce, code_verifier, return_to, expires_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		auth.HashToken(state), ls.Provider, ls.Nonce, ls.CodeVerifier, ls.ReturnTo, time.Now().Add(loginStateTTL))
	if err != nil {
		return fmt.Errorf("save login state: %w", err)
	}
	return nil
}

// TakeLoginState returns and deletes a pending sign-in, so each state works once.
func (s *Store) TakeLoginState(ctx context.Context, state string) (LoginState, error) {
	var ls LoginState
	err := s.pool.QueryRow(ctx,
		`DELETE FROM login_states WHERE state_hash = $1 AND expires_at > now() RETURNING provider, nonce, code_verifier, return_to`,
		auth.HashToken(state)).Scan(&ls.Provider, &ls.Nonce, &ls.CodeVerifier, &ls.ReturnTo)
	return ls, mapErr(err, "sign-in", "")
}

// Access settings and the audit log

func (s *Store) GetAccessSettings(ctx context.Context) (model.AccessSettings, error) {
	var a model.AccessSettings
	err := s.pool.QueryRow(ctx,
		`SELECT sso_auto_provision, auto_provision_role_id, callback_allowed_hosts FROM access_settings`).
		Scan(&a.SSOAutoProvision, &a.AutoProvisionRoleID, &a.CallbackAllowedHosts)
	a.CallbackAllowedHosts = orEmpty(a.CallbackAllowedHosts)
	if err != nil {
		return a, fmt.Errorf("read access settings: %w", err)
	}
	return a, nil
}

func (s *Store) UpdateAccessSettings(ctx context.Context, in model.AccessSettings) (model.AccessSettings, error) {
	var out model.AccessSettings
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`UPDATE access_settings SET sso_auto_provision = $1, auto_provision_role_id = $2, callback_allowed_hosts = $3,
			 updated_at = now()`, in.SSOAutoProvision, in.AutoProvisionRoleID, orEmpty(in.CallbackAllowedHosts)); err != nil {
			return mapErr(err, "access settings", "")
		}
		out = in
		out.CallbackAllowedHosts = orEmpty(in.CallbackAllowedHosts)
		return recordRevision(ctx, tx, "access_settings", "access_settings", "update", out)
	})
	return out, err
}

// AuditEntry is one sign-in related event.
type AuditEntry struct {
	Action    string
	ActorID   string
	ActorName string
	IP        string
	Details   map[string]any
}

func (s *Store) AppendAudit(ctx context.Context, e AuditEntry) error {
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO audit_events (action, actor_id, actor_name, ip, details) VALUES ($1, $2, $3, $4, $5)`,
		e.Action, nullableUUID(e.ActorID), e.ActorName, e.IP, e.Details)
	if err != nil {
		return fmt.Errorf("append audit event: %w", err)
	}
	return nil
}

func (s *Store) ListAudit(ctx context.Context, limit, offset int) ([]model.AuditEvent, int, error) {
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM audit_events`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count audit events: %w", err)
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, action, actor_id, actor_name, ip, details, created_at FROM audit_events ORDER BY id DESC LIMIT $1 OFFSET $2`,
		limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list audit events: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.AuditEvent, error) {
		var e model.AuditEvent
		err := r.Scan(&e.ID, &e.Action, &e.ActorID, &e.ActorName, &e.IP, &e.Details, &e.CreatedAt)
		return e, err
	})
	if err != nil {
		return nil, 0, fmt.Errorf("read audit events: %w", err)
	}
	return orEmpty(items), total, nil
}

// IsNotFound reports whether err means the row was missing.
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }

// StillValid reloads a principal and confirms its session or API key has not ended, for long-lived streams.
func (s *Store) StillValid(ctx context.Context, p *auth.Principal) (*auth.Principal, error) {
	fresh, err := s.LoadPrincipal(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	var alive bool
	switch {
	case p.SessionHash != "":
		err = s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sessions WHERE token_hash = $1 AND expires_at > now())`,
			p.SessionHash).Scan(&alive)
	case p.APIKeyID != "":
		// The key's limits apply to the reloaded account as they did when the stream began
		limited, err := s.WithKeyLimits(ctx, fresh, p.APIKeyID)
		if err != nil {
			return nil, err
		}
		return limited, nil
	default:
		alive = true
	}
	if err != nil {
		return nil, fmt.Errorf("check credentials: %w", err)
	}
	if !alive {
		return nil, ErrNotFound
	}
	fresh.Via, fresh.SessionHash = p.Via, p.SessionHash
	return fresh, nil
}
