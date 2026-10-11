package store

import (
	"context"
	"fmt"
)

// Administrator is a person who can manage people (admin.users), as the reset-admin-password command sees them.
type Administrator struct {
	ID     string
	Email  string
	Name   string
	Status string
}

// Administrators lists the people who hold admin.users, active or not, oldest first.
func (s *Store) Administrators(ctx context.Context) ([]Administrator, error) {
	rows, err := s.pool.Query(ctx, `SELECT u.id, coalesce(u.email, ''), u.name, u.status FROM users u
		WHERE u.kind = 'human' AND 'admin.users' = ANY(`+effectivePermissions+`)
		ORDER BY u.created_at, u.id`)
	if err != nil {
		return nil, fmt.Errorf("list administrators: %w", err)
	}
	defer rows.Close()
	var out []Administrator
	for rows.Next() {
		var a Administrator
		if err := rows.Scan(&a.ID, &a.Email, &a.Name, &a.Status); err != nil {
			return nil, fmt.Errorf("scan administrator: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
