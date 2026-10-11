package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"crewstudio/internal/model"
)

// ListPermissions returns the permission catalog as stored, in display order.
func (s *Store) ListPermissions(ctx context.Context) ([]model.PermissionDef, error) {
	rows, err := s.pool.Query(ctx, `SELECT key, feature, label, description FROM permissions ORDER BY position`)
	if err != nil {
		return nil, fmt.Errorf("list permissions: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.PermissionDef, error) {
		var p model.PermissionDef
		err := r.Scan(&p.Key, &p.Feature, &p.Label, &p.Description)
		return p, err
	})
	if err != nil {
		return nil, fmt.Errorf("read permissions: %w", err)
	}
	return orEmpty(items), nil
}

const roleSelect = `SELECT r.id, r.name, r.description, r.builtin,
	CASE WHEN r.builtin THEN ARRAY(SELECT key FROM permissions ORDER BY position)
	     ELSE ARRAY(SELECT rp.permission FROM role_permissions rp JOIN permissions p ON p.key = rp.permission
	                WHERE rp.role_id = r.id ORDER BY p.position) END,
	(SELECT count(*) FROM user_roles ur WHERE ur.role_id = r.id),
	r.created_at, r.updated_at FROM roles r`

func scanRole(row pgx.Row) (model.Role, error) {
	var r model.Role
	err := row.Scan(&r.ID, &r.Name, &r.Description, &r.Builtin, &r.Permissions, &r.UserCount, &r.CreatedAt, &r.UpdatedAt)
	r.Permissions = orEmpty(r.Permissions)
	return r, err
}

func (s *Store) ListRoles(ctx context.Context) ([]model.Role, error) {
	rows, err := s.pool.Query(ctx, roleSelect+` ORDER BY r.builtin DESC, r.name`)
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (model.Role, error) { return scanRole(r) })
	if err != nil {
		return nil, fmt.Errorf("read roles: %w", err)
	}
	return orEmpty(items), nil
}

func getRole(ctx context.Context, q queryer, id string) (model.Role, error) {
	r, err := scanRole(q.QueryRow(ctx, roleSelect+` WHERE r.id = $1`, id))
	return r, mapErr(err, "role", "")
}

func (s *Store) GetRole(ctx context.Context, id string) (model.Role, error) {
	return getRole(ctx, s.pool, id)
}

func replaceRolePermissions(ctx context.Context, tx pgx.Tx, roleID string, perms []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM role_permissions WHERE role_id = $1`, roleID); err != nil {
		return fmt.Errorf("clear role permissions: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO role_permissions (role_id, permission) SELECT $1, unnest($2::text[])`,
		roleID, orEmpty(perms)); err != nil {
		return mapErr(err, "role", "")
	}
	return nil
}

func (s *Store) CreateRole(ctx context.Context, in model.Role) (model.Role, error) {
	var out model.Role
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var id string
		if err := tx.QueryRow(ctx, `INSERT INTO roles (name, description) VALUES ($1, $2) RETURNING id`,
			in.Name, in.Description).Scan(&id); err != nil {
			return mapErr(err, "role", in.Name)
		}
		if err := replaceRolePermissions(ctx, tx, id, in.Permissions); err != nil {
			return err
		}
		var err error
		if out, err = getRole(ctx, tx, id); err != nil {
			return err
		}
		return recordRevision(ctx, tx, "role", id, "create", out)
	})
	return out, err
}

var errBuiltinRole = &ConflictError{Message: "The Administrator role is built in and cannot be changed or deleted."}

func (s *Store) UpdateRole(ctx context.Context, id string, in model.Role) (model.Role, error) {
	var out model.Role
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		old, err := getRole(ctx, tx, id)
		if err != nil {
			return err
		}
		if old.Builtin {
			return errBuiltinRole
		}
		if _, err := tx.Exec(ctx, `UPDATE roles SET name = $2, description = $3, updated_at = now() WHERE id = $1`,
			id, in.Name, in.Description); err != nil {
			return mapErr(err, "role", in.Name)
		}
		if err := replaceRolePermissions(ctx, tx, id, in.Permissions); err != nil {
			return err
		}
		if err := ensureAdminRemains(ctx, tx); err != nil {
			return err
		}
		if out, err = getRole(ctx, tx, id); err != nil {
			return err
		}
		return recordRevision(ctx, tx, "role", id, "update", out)
	})
	return out, err
}

func (s *Store) DeleteRole(ctx context.Context, id string) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		old, err := getRole(ctx, tx, id)
		if err != nil {
			return err
		}
		if old.Builtin {
			return errBuiltinRole
		}
		if _, err := tx.Exec(ctx, `DELETE FROM roles WHERE id = $1`, id); err != nil {
			return mapErr(err, "role", "")
		}
		if err := ensureAdminRemains(ctx, tx); err != nil {
			return err
		}
		return recordRevision(ctx, tx, "role", id, "delete", old)
	})
}
