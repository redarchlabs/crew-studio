package callbacks

import (
	"context"
	"log/slog"
	"time"

	"crewstudio/internal/storage"
	"crewstudio/internal/store"
)

const (
	housekeepingInterval = time.Minute
	deletionBatch        = 100
	// memoryPruneEvery is how often memories past their crew's retention (and unapproved pending ones) are deleted
	memoryPruneEvery = time.Hour
)

// Housekeeping removes stored files whose rows were deleted, expired sessions and sign-ins, and (hourly) memories
// past their retention.
type Housekeeping struct {
	store *store.Store
	files storage.Store
	log   *slog.Logger
	// lastPrune is when memories were last pruned; zero prunes on the first tick
	lastPrune time.Time
	now       func() time.Time
}

func NewHousekeeping(st *store.Store, files storage.Store, log *slog.Logger) *Housekeeping {
	return &Housekeeping{store: st, files: files, log: log, now: time.Now}
}

func (h *Housekeeping) Run(ctx context.Context) {
	ticker := time.NewTicker(housekeepingInterval)
	defer ticker.Stop()
	for {
		h.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (h *Housekeeping) Tick(ctx context.Context) {
	if err := h.store.PurgeExpired(ctx); err != nil && ctx.Err() == nil {
		h.log.Warn("purge expired sessions", "err", err)
	}
	h.pruneMemories(ctx)
	if h.files == nil {
		return
	}
	keys, err := h.store.PendingStorageDeletions(ctx, deletionBatch)
	if err != nil {
		if ctx.Err() == nil {
			h.log.Warn("list storage deletions", "err", err)
		}
		return
	}
	for _, key := range keys {
		delErr := h.files.Delete(ctx, key)
		if err := h.store.FinishStorageDeletion(ctx, key, delErr); err != nil {
			h.log.Warn("record storage deletion", "key", key, "err", err)
		}
	}
}

// pruneMemories deletes memories past retention at most once per memoryPruneEvery.
func (h *Housekeeping) pruneMemories(ctx context.Context) {
	now := h.now()
	if !h.lastPrune.IsZero() && now.Sub(h.lastPrune) < memoryPruneEvery {
		return
	}
	h.lastPrune = now
	n, err := h.store.PruneMemories(ctx)
	switch {
	case err != nil && ctx.Err() == nil:
		h.log.Warn("prune memories", "err", err)
	case n > 0:
		h.log.Info("pruned memories past their retention", "deleted", n)
	}
}
