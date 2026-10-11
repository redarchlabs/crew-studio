package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
)

// MaxActiveAPIKeys bounds the unrevoked, unexpired keys one account holds, so a leaked session cannot mint
// keys without limit.
const MaxActiveAPIKeys = 20

const apiKeyColumns = `id, user_id, name, prefix, created_at, expires_at, last_used_at, revoked_at`

func scanAPIKey(row pgx.Row) (model.APIKey, error) {
	var k model.APIKey
	err := row.Scan(&k.ID, &k.UserID, &k.Name, &k.Prefix, &k.CreatedAt, &k.ExpiresAt, &k.LastUsedAt, &k.RevokedAt)
	return k, err
}

// mayUseAPIKeys reports whether the account's keys still work: a person's keys need account.api_keys, so
// taking the permission away stops them.
func mayUseAPIKeys(p *auth.Principal) bool {
	return p.Kind != auth.KindHuman || p.Can(auth.AccountAPIKeys)
}

// CreateAPIKey issues a key for a service account and returns it in full (the only time it is available).
func (s *Store) CreateAPIKey(ctx context.Context, userID, name string, expiresAt *time.Time) (model.APIKey, string, error) {
	return s.createAPIKey(ctx, userID, auth.KindService, name, expiresAt)
}

// CreatePersonalAPIKey issues a key that acts as a person; callers check that they may hold one.
func (s *Store) CreatePersonalAPIKey(ctx context.Context, userID, name string, expiresAt *time.Time) (model.APIKey, string, error) {
	return s.createAPIKey(ctx, userID, auth.KindHuman, name, expiresAt)
}

func (s *Store) createAPIKey(ctx context.Context, userID, kind, name string, expiresAt *time.Time) (model.APIKey, string, error) {
	key, prefix := auth.NewKey(auth.APIKeyPrefix)
	actor := actorFrom(ctx)
	var out model.APIKey
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		// Locking the account row makes the active-key count exact under concurrent requests
		var have string
		if err := tx.QueryRow(ctx, `SELECT kind FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&have); err != nil {
			return mapErr(err, "account", "")
		}
		if have != kind {
			if kind == auth.KindService {
				return &ConflictError{Message: "API keys can only be issued to service accounts here."}
			}
			return &ConflictError{Message: "Personal API keys belong to people, not service accounts."}
		}
		var active int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM api_keys WHERE user_id = $1 AND revoked_at IS NULL
			AND (expires_at IS NULL OR expires_at > now())`, userID).Scan(&active); err != nil {
			return fmt.Errorf("count api keys: %w", err)
		}
		if active >= MaxActiveAPIKeys {
			return &ConflictError{Message: fmt.Sprintf("This account already has %d active API keys; revoke one first.", MaxActiveAPIKeys)}
		}
		var err error
		out, err = scanAPIKey(tx.QueryRow(ctx,
			`INSERT INTO api_keys (user_id, name, prefix, secret_hash, created_by, expires_at)
			 VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+apiKeyColumns,
			userID, name, prefix, auth.HashToken(key), nullableUUID(actor.ID), expiresAt))
		if err != nil {
			return mapErr(err, "API key", name)
		}
		return recordRevision(ctx, tx, "api_key", out.ID, "create", out)
	})
	return out, key, err
}

// APIKeyFilter selects a page of keys. Kinds says whose keys are visible (people, service accounts); with
// none, nothing is.
type APIKeyFilter struct {
	UserID string
	Kinds  []string
	// Kind narrows to people ("human") or service accounts ("service")
	Kind string
	// Status is "active", "expired" or "revoked"
	Status string
	// Query matches the key name, account name or prefix
	Query  string
	Limit  int
	Offset int
}

const apiKeyStatus = `CASE WHEN k.revoked_at IS NOT NULL THEN 'revoked'
	WHEN k.expires_at IS NOT NULL AND k.expires_at <= now() THEN 'expired' ELSE 'active' END`

// ListAPIKeys returns a page of keys with their accounts, active keys first and each group newest first, and the
// number of keys that match.
func (s *Store) ListAPIKeys(ctx context.Context, f APIKeyFilter) ([]model.AccountAPIKey, int, error) {
	const from = ` FROM api_keys k JOIN users u ON u.id = k.user_id LEFT JOIN users cb ON cb.id = k.created_by`
	where := ` WHERE u.kind = ANY($1) AND ($2::uuid IS NULL OR k.user_id = $2) AND ($3::text IS NULL OR u.kind = $3)
		AND ($4::text IS NULL OR ` + apiKeyStatus + ` = $4)
		AND ($5::text IS NULL OR k.name ILIKE '%' || $5 || '%' OR u.name ILIKE '%' || $5 || '%' OR k.prefix ILIKE '%' || $5 || '%')`
	args := []any{orEmpty(f.Kinds), nullableUUID(f.UserID), nullableText(f.Kind), nullableText(f.Status),
		nullableText(escapeLike(strings.TrimSpace(f.Query)))}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+from+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count api keys: %w", err)
	}
	rows, err := s.pool.Query(ctx, `SELECT k.id, k.user_id, k.name, k.prefix, k.created_at, k.expires_at, k.last_used_at,
			k.revoked_at, u.name, u.kind, cb.name`+from+where+`
		ORDER BY (`+apiKeyStatus+`) = 'active' DESC, k.created_at DESC, k.id LIMIT $6 OFFSET $7`,
		append(args, f.Limit, f.Offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list api keys: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.AccountAPIKey, error) {
		var k model.AccountAPIKey
		err := r.Scan(&k.ID, &k.UserID, &k.Name, &k.Prefix, &k.CreatedAt, &k.ExpiresAt, &k.LastUsedAt, &k.RevokedAt,
			&k.UserName, &k.UserKind, &k.CreatedByName)
		return k, err
	})
	if err != nil {
		return nil, 0, fmt.Errorf("read api keys: %w", err)
	}
	return orEmpty(items), total, nil
}

// UsersByID loads several accounts at once, keyed by id; ids that do not exist are left out.
func (s *Store) UsersByID(ctx context.Context, ids []string) (map[string]model.User, error) {
	rows, err := s.pool.Query(ctx, userSelect+` WHERE u.id = ANY($1::uuid[])`, orEmpty(ids))
	if err != nil {
		return nil, fmt.Errorf("load users: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.User, error) { return scanUser(r) })
	if err != nil {
		return nil, fmt.Errorf("read users: %w", err)
	}
	out := make(map[string]model.User, len(items))
	for _, u := range items {
		out[u.ID] = u
	}
	return out, nil
}

// RevokeAPIKey revokes a key of the account. Revoking it again keeps the first time; changed reports whether this
// call did the revoking, so history and the audit log record it once.
func (s *Store) RevokeAPIKey(ctx context.Context, userID, keyID string) (key model.APIKey, changed bool, err error) {
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		changed = false
		var err error
		key, err = scanAPIKey(tx.QueryRow(ctx,
			`SELECT `+apiKeyColumns+` FROM api_keys WHERE id = $1 AND user_id = $2 FOR UPDATE`, keyID, userID))
		if err != nil {
			return mapErr(err, "API key", "")
		}
		if key.RevokedAt != nil {
			return nil
		}
		key, err = scanAPIKey(tx.QueryRow(ctx, `UPDATE api_keys SET revoked_at = now() WHERE id = $1 RETURNING `+apiKeyColumns, keyID))
		if err != nil {
			return mapErr(err, "API key", "")
		}
		changed = true
		return recordRevision(ctx, tx, "api_key", key.ID, "update", key)
	})
	return key, changed, err
}

// APIKeyPrincipal resolves a presented API key. Unknown, revoked or expired keys, and personal keys of people
// who no longer hold account.api_keys, return ErrNotFound.
func (s *Store) APIKeyPrincipal(ctx context.Context, key string) (*auth.Principal, error) {
	prefix, ok := auth.SplitKey(auth.APIKeyPrefix, key)
	if !ok {
		return nil, ErrNotFound
	}
	var id, userID, hash string
	var lastUsed *time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, secret_hash, last_used_at FROM api_keys
		 WHERE prefix = $1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())`, prefix).
		Scan(&id, &userID, &hash, &lastUsed)
	if err != nil {
		return nil, mapErr(err, "API key", "")
	}
	if !auth.TokensEqual(hash, auth.HashToken(key)) {
		return nil, ErrNotFound
	}
	p, err := s.LoadPrincipal(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !mayUseAPIKeys(p) {
		return nil, ErrNotFound
	}
	if lastUsed == nil || time.Since(*lastUsed) > sessionTouchEvery {
		if _, err := s.pool.Exec(ctx, `UPDATE api_keys SET last_used_at = now() WHERE id = $1`, id); err != nil {
			return nil, fmt.Errorf("touch api key: %w", err)
		}
	}
	p.Via = auth.ViaAPIKey
	p.APIKeyID = id
	return p, nil
}
