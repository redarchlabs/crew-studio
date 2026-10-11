package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

// SealedSecret is a connection's secret fields, already encrypted by the caller (see internal/connections).
type SealedSecret struct {
	Cipher []byte
	// Keys are the names of the secret fields inside, kept in clear so lists need no decryption
	Keys []string
}

const connectionSelect = `SELECT x.id, x.name, x.kind, x.config, x.secret_keys, x.secret_updated_at, x.area_ids::text[],
	x.enabled, (SELECT count(*) FROM crew_connections cc WHERE cc.connection_id = x.id)::int,
	x.created_by, x.created_at, x.updated_at FROM connections x`

func scanConnection(row pgx.Row) (model.Connection, error) {
	var c model.Connection
	err := row.Scan(&c.ID, &c.Name, &c.Kind, &c.Config, &c.SecretKeys, &c.SecretUpdatedAt, &c.AreaIDs, &c.Enabled,
		&c.CrewCount, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt)
	if c.Config == nil {
		c.Config = map[string]string{}
	}
	c.SecretKeys = orEmpty(c.SecretKeys)
	c.AreaIDs = orEmpty(c.AreaIDs)
	c.MissingSecrets = model.MissingSecrets(c.Kind, c.SecretKeys)
	return c, err
}

func collectConnections(rows pgx.Rows, err error) ([]model.Connection, error) {
	if err != nil {
		return nil, fmt.Errorf("list connections: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.Connection, error) { return scanConnection(r) })
	if err != nil {
		return nil, fmt.Errorf("read connections: %w", err)
	}
	return orEmpty(items), nil
}

// ListConnections returns the connections that share an area with the scope.
func (s *Store) ListConnections(ctx context.Context, scope Scope) ([]model.Connection, error) {
	all, areas := scope.areaArgs()
	return collectConnections(s.pool.Query(ctx, connectionSelect+` WHERE $1 OR x.area_ids && $2::uuid[] ORDER BY x.name`, all, areas))
}

func getConnection(ctx context.Context, q queryer, id string) (model.Connection, error) {
	c, err := scanConnection(q.QueryRow(ctx, connectionSelect+` WHERE x.id = $1`, id))
	return c, mapErr(err, "connection", "")
}

func (s *Store) GetConnection(ctx context.Context, id string) (model.Connection, error) {
	return getConnection(ctx, s.pool, id)
}

// CreateConnection stores a new connection under id (chosen by the caller, because secrets are sealed to it).
func (s *Store) CreateConnection(ctx context.Context, id string, in model.ConnectionInput, secret *SealedSecret) (model.Connection, error) {
	config, err := json.Marshal(in.Config)
	if err != nil {
		return model.Connection{}, fmt.Errorf("encode connection config: %w", err)
	}
	var cipher []byte
	keys := []string{}
	var sealedAt *time.Time
	if secret != nil {
		now := time.Now()
		cipher, keys, sealedAt = secret.Cipher, orEmpty(secret.Keys), &now
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO connections (id, name, kind, config, secret_cipher, secret_keys, secret_updated_at, area_ids, enabled, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		id, in.Name, in.Kind, config, cipher, keys, sealedAt, orEmpty(in.AreaIDs), enabledOf(in), nullableUUID(actorFrom(ctx).ID))
	if err != nil {
		return model.Connection{}, mapErr(err, "connection", in.Name)
	}
	return s.GetConnection(ctx, id)
}

func enabledOf(in model.ConnectionInput) bool {
	return in.Enabled == nil || *in.Enabled
}

// UpdateConnection changes a connection's name, settings, areas and enabled flag. Its kind stays, and so do its
// secrets unless clearSecrets is set.
func (s *Store) UpdateConnection(ctx context.Context, id string, in model.ConnectionInput, clearSecrets bool) (model.Connection, error) {
	config, err := json.Marshal(in.Config)
	if err != nil {
		return model.Connection{}, fmt.Errorf("encode connection config: %w", err)
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE connections SET name = $2, config = $3, area_ids = $4, enabled = $5, updated_at = now(),
		 secret_cipher = CASE WHEN $6 THEN NULL ELSE secret_cipher END,
		 secret_keys = CASE WHEN $6 THEN '{}' ELSE secret_keys END,
		 secret_updated_at = CASE WHEN $6 THEN NULL ELSE secret_updated_at END
		 WHERE id = $1`,
		id, in.Name, config, orEmpty(in.AreaIDs), enabledOf(in), clearSecrets)
	if err != nil {
		return model.Connection{}, mapErr(err, "connection", in.Name)
	}
	if tag.RowsAffected() == 0 {
		return model.Connection{}, ErrNotFound
	}
	return s.GetConnection(ctx, id)
}

// ReplaceConnectionSecret swaps every secret field of a connection for a newly sealed set.
func (s *Store) ReplaceConnectionSecret(ctx context.Context, id string, secret SealedSecret) (model.Connection, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE connections SET secret_cipher = $2, secret_keys = $3, secret_updated_at = now(), updated_at = now() WHERE id = $1`,
		id, secret.Cipher, orEmpty(secret.Keys))
	if err != nil {
		return model.Connection{}, fmt.Errorf("replace connection secret: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return model.Connection{}, ErrNotFound
	}
	return s.GetConnection(ctx, id)
}

// ConnectionCipher returns a connection's sealed secret (nil when none is set).
func (s *Store) ConnectionCipher(ctx context.Context, id string) ([]byte, error) {
	var cipher []byte
	err := s.pool.QueryRow(ctx, `SELECT secret_cipher FROM connections WHERE id = $1`, id).Scan(&cipher)
	return cipher, mapErr(err, "connection", "")
}

// DeleteConnection deletes a connection. Items removed from tracking lists only keep history, so they go with it;
// items still tracked (like watches reading it) keep it in use.
func (s *Store) DeleteConnection(ctx context.Context, id string) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM tracking_items WHERE connection_id = $1 AND removed_at IS NOT NULL`, id); err != nil {
			return fmt.Errorf("delete removed tracking items: %w", err)
		}
		tag, err := tx.Exec(ctx, `DELETE FROM connections WHERE id = $1`, id)
		if err != nil {
			return mapErr(err, "connection", "")
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// AnySealedConnection returns one connection that has a sealed secret, for checking the key at startup.
// found is false when no connection exists at all; id is "" when connections exist but none has a secret.
func (s *Store) AnySealedConnection(ctx context.Context) (id string, cipher []byte, found bool, err error) {
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM connections`).Scan(&count); err != nil {
		return "", nil, false, fmt.Errorf("count connections: %w", err)
	}
	if count == 0 {
		return "", nil, false, nil
	}
	err = s.pool.QueryRow(ctx,
		`SELECT id, secret_cipher FROM connections WHERE secret_cipher IS NOT NULL ORDER BY created_at LIMIT 1`).Scan(&id, &cipher)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, true, nil
	}
	if err != nil {
		return "", nil, true, fmt.Errorf("read a connection secret: %w", err)
	}
	return id, cipher, true, nil
}

// CrewConnections returns the connections a crew may use that the scope can see.
func (s *Store) CrewConnections(ctx context.Context, crewID string, scope Scope) ([]model.Connection, error) {
	all, areas := scope.areaArgs()
	return collectConnections(s.pool.Query(ctx, connectionSelect+`
		WHERE ($1 OR x.area_ids && $2::uuid[]) AND EXISTS (SELECT 1 FROM crew_connections cc WHERE cc.crew_id = $3 AND cc.connection_id = x.id)
		ORDER BY x.name`, all, areas, crewID))
}

// SetCrewConnections makes ids the crew's connections among those the scope can see. Links to connections
// outside the scope are kept, so nobody removes what they cannot see.
func (s *Store) SetCrewConnections(ctx context.Context, crewID string, ids []string, scope Scope) error {
	all, areas := scope.areaArgs()
	return s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM crew_connections cc USING connections x
			WHERE cc.crew_id = $3 AND cc.connection_id = x.id AND ($1 OR x.area_ids && $2::uuid[])`, all, areas, crewID); err != nil {
			return fmt.Errorf("clear crew connections: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO crew_connections (crew_id, connection_id)
			SELECT $1, unnest($2::uuid[]) ON CONFLICT DO NOTHING`, crewID, orEmpty(ids)); err != nil {
			return mapErr(err, "crew connection", "")
		}
		return nil
	})
}

// ResealConnectionSecrets re-encrypts every connection secret in one transaction: reseal gets each connection's id
// and sealed secret and returns it sealed again (with a new key). Any failure rolls everything back, so secrets are
// never left under two different keys. It returns how many secrets were re-encrypted.
func (s *Store) ResealConnectionSecrets(ctx context.Context, reseal func(id string, sealed []byte) ([]byte, error)) (int, error) {
	count := 0
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id, secret_cipher FROM connections WHERE secret_cipher IS NOT NULL ORDER BY id FOR UPDATE`)
		if err != nil {
			return fmt.Errorf("read connection secrets: %w", err)
		}
		type sealed struct {
			id     string
			cipher []byte
		}
		var all []sealed
		for rows.Next() {
			var x sealed
			if err := rows.Scan(&x.id, &x.cipher); err != nil {
				rows.Close()
				return fmt.Errorf("read connection secrets: %w", err)
			}
			all = append(all, x)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("read connection secrets: %w", err)
		}
		for _, x := range all {
			next, err := reseal(x.id, x.cipher)
			if err != nil {
				return fmt.Errorf("connection %s: %w", x.id, err)
			}
			if _, err := tx.Exec(ctx, `UPDATE connections SET secret_cipher = $2 WHERE id = $1`, x.id, next); err != nil {
				return fmt.Errorf("save connection %s: %w", x.id, err)
			}
			count++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}
