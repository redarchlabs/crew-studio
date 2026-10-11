package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// LoadHelpVectors returns the cached help-section vectors for these content hashes.
func (s *Store) LoadHelpVectors(ctx context.Context, model string, hashes []string) (map[string][]float32, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT content_hash, vector FROM help_embeddings WHERE model = $1 AND content_hash = ANY($2)`, model, hashes)
	if err != nil {
		return nil, fmt.Errorf("load help vectors: %w", err)
	}
	out := map[string][]float32{}
	for rows.Next() {
		var hash string
		var vec []float32
		if err := rows.Scan(&hash, &vec); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan help vector: %w", err)
		}
		out[hash] = vec
	}
	return out, rows.Err()
}

// SaveHelpVectors caches help-section vectors.
func (s *Store) SaveHelpVectors(ctx context.Context, model string, vectors map[string][]float32) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		batch := &pgx.Batch{}
		for hash, vec := range vectors {
			batch.Queue(`INSERT INTO help_embeddings (model, content_hash, vector) VALUES ($1, $2, $3)
				ON CONFLICT (model, content_hash) DO UPDATE SET vector = EXCLUDED.vector`, model, hash, vec)
		}
		if err := tx.SendBatch(ctx, batch).Close(); err != nil {
			return fmt.Errorf("save help vectors: %w", err)
		}
		return nil
	})
}

// PruneHelpVectors deletes cached vectors for other models and for content that no longer exists.
func (s *Store) PruneHelpVectors(ctx context.Context, model string, keep []string) error {
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM help_embeddings WHERE model <> $1 OR NOT (content_hash = ANY($2))`, model, keep); err != nil {
		return fmt.Errorf("prune help vectors: %w", err)
	}
	return nil
}
