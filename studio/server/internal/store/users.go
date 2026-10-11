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

// effectivePermissions is a SQL array of everything user u holds: the built-in Administrator role grants the
// whole catalog; other roles and direct grants add their permissions.
const effectivePermissions = `ARRAY(SELECT p.key FROM permissions p WHERE
	EXISTS (SELECT 1 FROM user_roles ur JOIN roles r ON r.id = ur.role_id WHERE ur.user_id = u.id AND r.builtin)
	OR EXISTS (SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_id = ur.role_id
	           WHERE ur.user_id = u.id AND rp.permission = p.key)
	OR EXISTS (SELECT 1 FROM user_permissions up WHERE up.user_id = u.id AND up.permission = p.key)
	ORDER BY p.position)`

const userSelect = `SELECT u.id, u.kind, u.email, u.name, u.status, u.all_areas,
	ARRAY(SELECT area_id::text FROM user_areas WHERE user_id = u.id ORDER BY area_id),
	ARRAY(SELECT role_id::text FROM user_roles WHERE user_id = u.id ORDER BY role_id),
	ARRAY(SELECT up.permission FROM user_permissions up JOIN permissions p ON p.key = up.permission
	      WHERE up.user_id = u.id ORDER BY p.position),
	` + effectivePermissions + `,
	u.password_hash IS NOT NULL,
	u.last_login_at, u.created_at, u.updated_at FROM users u`

func scanUser(row pgx.Row) (model.User, error) {
	var u model.User
	err := row.Scan(&u.ID, &u.Kind, &u.Email, &u.Name, &u.Status, &u.AllAreas, &u.AreaIDs, &u.RoleIDs, &u.Permissions,
		&u.EffectivePermissions, &u.HasPassword, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
	u.AreaIDs = orEmpty(u.AreaIDs)
	u.RoleIDs = orEmpty(u.RoleIDs)
	u.Permissions = orEmpty(u.Permissions)
	u.EffectivePermissions = orEmpty(u.EffectivePermissions)
	u.Identities = []model.LinkedIdentity{}
	return u, err
}

// ListUsers returns people (kind "human") or service accounts (kind "service").
func (s *Store) ListUsers(ctx context.Context, kind string) ([]model.User, error) {
	rows, err := s.pool.Query(ctx, userSelect+` WHERE u.kind = $1 ORDER BY lower(u.name)`, kind)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.User, error) { return scanUser(r) })
	if err != nil {
		return nil, fmt.Errorf("read users: %w", err)
	}
	return orEmpty(items), nil
}

func getUser(ctx context.Context, q queryer, id string) (model.User, error) {
	u, err := scanUser(q.QueryRow(ctx, userSelect+` WHERE u.id = $1`, id))
	if err != nil {
		return u, mapErr(err, "user", "")
	}
	rows, err := q.Query(ctx,
		`SELECT provider, email, last_login_at FROM user_identities WHERE user_id = $1 ORDER BY provider`, id)
	if err != nil {
		return u, fmt.Errorf("list identities: %w", err)
	}
	u.Identities, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.LinkedIdentity, error) {
		var li model.LinkedIdentity
		err := r.Scan(&li.Provider, &li.Email, &li.LastLoginAt)
		return li, err
	})
	if err != nil {
		return u, fmt.Errorf("read identities: %w", err)
	}
	u.Identities = orEmpty(u.Identities)
	return u, nil
}

func (s *Store) GetUser(ctx context.Context, id string) (model.User, error) {
	return getUser(ctx, s.pool, id)
}

func writeUserGrants(ctx context.Context, tx pgx.Tx, id string, in model.UserInput) error {
	for _, stmt := range []string{
		`DELETE FROM user_roles WHERE user_id = $1`, `DELETE FROM user_permissions WHERE user_id = $1`,
		`DELETE FROM user_areas WHERE user_id = $1`,
	} {
		if _, err := tx.Exec(ctx, stmt, id); err != nil {
			return fmt.Errorf("clear user grants: %w", err)
		}
	}
	inserts := []struct {
		sql  string
		vals []string
	}{
		{`INSERT INTO user_roles (user_id, role_id) SELECT $1, unnest($2::uuid[])`, in.RoleIDs},
		{`INSERT INTO user_permissions (user_id, permission) SELECT $1, unnest($2::text[])`, in.Permissions},
		{`INSERT INTO user_areas (user_id, area_id) SELECT $1, unnest($2::uuid[])`, in.AreaIDs},
	}
	for _, ins := range inserts {
		if _, err := tx.Exec(ctx, ins.sql, id, orEmpty(ins.vals)); err != nil {
			return mapErr(err, "user", "")
		}
	}
	return nil
}

// ErrLastAdmin stops a change that would leave nobody able to manage users.
var ErrLastAdmin = &ConflictError{Message: "At least one active person must keep the Manage users permission.", Code: "last_admin"}

func ensureAdminRemains(ctx context.Context, q queryer) error {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM users u WHERE u.kind = 'human' AND u.status = 'active'
		AND 'admin.users' = ANY(`+effectivePermissions+`)`).Scan(&n)
	if err != nil {
		return fmt.Errorf("count administrators: %w", err)
	}
	if n == 0 {
		return ErrLastAdmin
	}
	return nil
}

func emailOrNil(in model.UserInput) *string {
	if in.Kind == auth.KindService {
		return nil
	}
	e := strings.ToLower(in.Email)
	return &e
}

func userConflict(err error, in model.UserInput) error {
	name := in.Email
	if in.Kind == auth.KindService {
		name = in.Name
	}
	return mapErr(err, "user", name)
}

// CreateUser adds a person or service account. Service accounts get a callback signing secret.
func (s *Store) CreateUser(ctx context.Context, in model.UserInput) (model.User, error) {
	var out model.User
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var secret *string
		if in.Kind == auth.KindService {
			v := auth.RandomToken(32)
			secret = &v
		}
		var id string
		if err := tx.QueryRow(ctx,
			`INSERT INTO users (kind, email, name, status, all_areas, signing_secret) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			in.Kind, emailOrNil(in), in.Name, in.Status, in.AllAreas, secret).Scan(&id); err != nil {
			return userConflict(err, in)
		}
		if err := writeUserGrants(ctx, tx, id, in); err != nil {
			return err
		}
		var err error
		if out, err = getUser(ctx, tx, id); err != nil {
			return err
		}
		return recordRevision(ctx, tx, "user", id, "create", out)
	})
	return out, err
}

// UpdateUser changes a user. Disabling signs them out everywhere.
func (s *Store) UpdateUser(ctx context.Context, id string, in model.UserInput) (model.User, error) {
	var out model.User
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE users SET email = $2, name = $3, status = $4, all_areas = $5, updated_at = now() WHERE id = $1 AND kind = $6`,
			id, emailOrNil(in), in.Name, in.Status, in.AllAreas, in.Kind)
		if err != nil {
			return userConflict(err, in)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		if err := writeUserGrants(ctx, tx, id, in); err != nil {
			return err
		}
		if in.Status == model.UserDisabled {
			if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, id); err != nil {
				return fmt.Errorf("end sessions: %w", err)
			}
		}
		if err := ensureAdminRemains(ctx, tx); err != nil {
			return err
		}
		if out, err = getUser(ctx, tx, id); err != nil {
			return err
		}
		return recordRevision(ctx, tx, "user", id, "update", out)
	})
	return out, err
}

func (s *Store) DeleteUser(ctx context.Context, id string) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		old, err := getUser(ctx, tx, id)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, id); err != nil {
			return mapErr(err, "user", "")
		}
		if err := ensureAdminRemains(ctx, tx); err != nil {
			return err
		}
		return recordRevision(ctx, tx, "user", id, "delete", old)
	})
}

// SetPassword sets (or with nil clears) a person's local password and resets any lockout.
func (s *Store) SetPassword(ctx context.Context, id string, hash *string) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE users SET password_hash = $2, failed_logins = 0, locked_until = NULL, updated_at = now()
			 WHERE id = $1 AND kind = 'human'`, id, hash)
		if err != nil {
			return fmt.Errorf("set password: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		action := "password_set"
		if hash == nil {
			action = "password_cleared"
		}
		return recordRevision(ctx, tx, "user", id, "update", map[string]string{"id": id, "change": action})
	})
}

// LoadPrincipal resolves an active user's permissions and areas.
func (s *Store) LoadPrincipal(ctx context.Context, userID string) (*auth.Principal, error) {
	return loadPrincipal(ctx, s.pool, userID)
}

func loadPrincipal(ctx context.Context, q queryer, userID string) (*auth.Principal, error) {
	p := &auth.Principal{Permissions: map[string]bool{}}
	var status string
	var email *string
	var perms []string
	err := q.QueryRow(ctx, `SELECT u.id, u.name, u.email, u.kind, u.status, u.all_areas,
		ARRAY(SELECT area_id::text FROM user_areas WHERE user_id = u.id), `+effectivePermissions+`
		FROM users u WHERE u.id = $1`, userID).
		Scan(&p.UserID, &p.Name, &email, &p.Kind, &status, &p.AllAreas, &p.AreaIDs, &perms)
	if err != nil {
		return nil, mapErr(err, "user", "")
	}
	if status != model.UserActive {
		return nil, ErrNotFound
	}
	if email != nil {
		p.Email = *email
	}
	p.AreaIDs = orEmpty(p.AreaIDs)
	for _, perm := range perms {
		p.Permissions[perm] = true
	}
	return p, nil
}

// HumanUserCount is used to decide whether first-run setup is needed.
func (s *Store) HumanUserCount(ctx context.Context) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE kind = 'human'`).Scan(&n); err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

// ErrSetupDone means someone already created the first administrator.
var ErrSetupDone = &ConflictError{Message: "Crew Studio is already set up. Sign in instead."}

// CreateFirstAdmin creates the first person as an Administrator over all areas, only while there are no people.
func (s *Store) CreateFirstAdmin(ctx context.Context, email, name, passwordHash string) (string, error) {
	var id string
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		// Serialise concurrent setup attempts
		if _, err := tx.Exec(ctx, `LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE`); err != nil {
			return fmt.Errorf("lock users: %w", err)
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE kind = 'human'`).Scan(&n); err != nil {
			return fmt.Errorf("count users: %w", err)
		}
		if n > 0 {
			return ErrSetupDone
		}
		if err := tx.QueryRow(ctx,
			`INSERT INTO users (kind, email, name, all_areas, password_hash) VALUES ('human', $1, $2, true, $3) RETURNING id`,
			strings.ToLower(email), name, passwordHash).Scan(&id); err != nil {
			return mapErr(err, "user", email)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, id, auth.AdministratorRoleID); err != nil {
			return fmt.Errorf("grant administrator: %w", err)
		}
		u, err := getUser(ctx, tx, id)
		if err != nil {
			return err
		}
		return recordRevision(ctx, tx, "user", id, "create", u)
	})
	return id, err
}

// LocalLogin is what password sign-in needs to know about an account.
type LocalLogin struct {
	UserID       string
	Name         string
	PasswordHash string
	Active       bool
	LockedUntil  *time.Time
}

// FindLocalLogin looks up a person by email for password sign-in. A missing account returns ErrNotFound.
func (s *Store) FindLocalLogin(ctx context.Context, email string) (LocalLogin, error) {
	var l LocalLogin
	var hash *string
	var status string
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, password_hash, status, locked_until FROM users WHERE kind = 'human' AND lower(email) = lower($1)`,
		email).Scan(&l.UserID, &l.Name, &hash, &status, &l.LockedUntil)
	if err != nil {
		return l, mapErr(err, "user", "")
	}
	if hash != nil {
		l.PasswordHash = *hash
	}
	l.Active = status == model.UserActive
	return l, nil
}

const (
	maxFailedLogins = 5
	lockoutDuration = 15 * time.Minute
)

// RecordLoginFailure counts a wrong password and locks the account for a while after too many.
func (s *Store) RecordLoginFailure(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET failed_logins = failed_logins + 1,
		 locked_until = CASE WHEN failed_logins + 1 >= $2 THEN now() + $3::interval ELSE locked_until END
		 WHERE id = $1`, userID, maxFailedLogins, fmt.Sprintf("%d seconds", int(lockoutDuration.Seconds())))
	if err != nil {
		return fmt.Errorf("record failed sign-in: %w", err)
	}
	return nil
}

func (s *Store) RecordLoginSuccess(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET failed_logins = 0, locked_until = NULL, last_login_at = now() WHERE id = $1`, userID)
	if err != nil {
		return fmt.Errorf("record sign-in: %w", err)
	}
	return nil
}

// ErrNoAccount means an SSO identity matched no one and auto-provisioning is off.
var ErrNoAccount = errors.New("no Crew Studio account for this identity")

// ResolveSSOUser finds (or links, or creates) the user for an SSO identity:
// 1. a previously linked identity; 2. a person with the same email, who gets this identity linked;
// 3. when auto-provisioning is on, a new person with the configured role and no areas.
func (s *Store) ResolveSSOUser(ctx context.Context, id auth.Identity) (string, error) {
	// Simultaneous first sign-ins race to create the same person or link the same identity; the losers hit a
	// unique index and simply try again, which then finds what the winner created.
	const attempts = 3
	for i := 1; ; i++ {
		userID, err := s.resolveSSOUserOnce(ctx, id)
		if err == nil || i == attempts || !isUniqueViolation(err) {
			return userID, err
		}
	}
}

func (s *Store) resolveSSOUserOnce(ctx context.Context, id auth.Identity) (string, error) {
	var userID string
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx,
			`UPDATE user_identities SET last_login_at = now(), email = $3 WHERE provider = $1 AND subject = $2 RETURNING user_id`,
			id.Provider, id.Subject, id.Email).Scan(&userID)
		if err == nil {
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("find identity: %w", err)
		}
		err = tx.QueryRow(ctx, `SELECT id FROM users WHERE kind = 'human' AND lower(email) = $1`, id.Email).Scan(&userID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if userID, err = provisionUser(ctx, tx, id); err != nil {
				return err
			}
		case err != nil:
			return fmt.Errorf("find user by email: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO user_identities (provider, subject, user_id, email) VALUES ($1, $2, $3, $4)`,
			id.Provider, id.Subject, userID, id.Email); err != nil {
			return fmt.Errorf("link identity: %w", err)
		}
		return nil
	})
	return userID, err
}

func provisionUser(ctx context.Context, tx pgx.Tx, id auth.Identity) (string, error) {
	var enabled bool
	var roleID *string
	if err := tx.QueryRow(ctx, `SELECT sso_auto_provision, auto_provision_role_id FROM access_settings`).
		Scan(&enabled, &roleID); err != nil {
		return "", fmt.Errorf("read access settings: %w", err)
	}
	if !enabled {
		return "", ErrNoAccount
	}
	var userID string
	if err := tx.QueryRow(ctx, `INSERT INTO users (kind, email, name) VALUES ('human', $1, $2) RETURNING id`,
		id.Email, id.Name).Scan(&userID); err != nil {
		return "", fmt.Errorf("create user: %w", err)
	}
	if roleID != nil {
		if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, userID, *roleID); err != nil {
			return "", fmt.Errorf("grant default role: %w", err)
		}
	}
	u, err := getUser(ctx, tx, userID)
	if err != nil {
		return "", err
	}
	ctx = WithActor(ctx, Actor{ID: userID, Name: id.Name + " (auto-provisioned via " + id.Provider + ")"})
	return userID, recordRevision(ctx, tx, "user", userID, "create", u)
}

// SigningSecret returns a service account's callback signing secret.
func (s *Store) SigningSecret(ctx context.Context, userID string) (string, error) {
	var secret *string
	err := s.pool.QueryRow(ctx, `SELECT signing_secret FROM users WHERE id = $1 AND kind = 'service'`, userID).Scan(&secret)
	if err != nil {
		return "", mapErr(err, "service account", "")
	}
	if secret == nil {
		return "", ErrNotFound
	}
	return *secret, nil
}

// RotateSigningSecret replaces a service account's signing secret and returns the new one.
func (s *Store) RotateSigningSecret(ctx context.Context, userID string) (string, error) {
	secret := auth.RandomToken(32)
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE users SET signing_secret = $2, updated_at = now() WHERE id = $1 AND kind = 'service'`,
			userID, secret)
		if err != nil {
			return fmt.Errorf("rotate signing secret: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return recordRevision(ctx, tx, "user", userID, "update", map[string]string{"id": userID, "change": "signing_secret_rotated"})
	})
	return secret, err
}
