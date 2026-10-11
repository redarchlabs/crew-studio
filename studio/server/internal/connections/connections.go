// Package connections stores credentials for outside systems: it seals secret fields with the server's
// STUDIO_SECRETS_KEY before they reach the database, checks that key at startup, and tests connections.
package connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/secrets"
	"crewstudio/internal/store"
)

const defaultTestTimeout = 10 * time.Second

// errNoKey is returned when a secret must be sealed or opened but the server has no key.
var errNoKey = &store.ConflictError{Code: "secrets_key_missing",
	Message: "Connections are turned off until STUDIO_SECRETS_KEY is set on the server (32 random bytes, base64). " +
		"Ask whoever runs Crew Studio to set it and restart."}

// Service creates connections and reads or replaces their secrets.
type Service struct {
	store   *store.Store
	box     *secrets.Box
	timeout time.Duration
}

// New builds the service. box may be nil when STUDIO_SECRETS_KEY is not set; then nothing can be saved.
func New(st *store.Store, box *secrets.Box) *Service {
	return &Service{store: st, box: box, timeout: defaultTestTimeout}
}

// Ready reports whether secrets can be sealed (STUDIO_SECRETS_KEY is set).
func (s *Service) Ready() bool { return s.box != nil }

// CheckKey runs at startup. Without a key it returns nil, unless connections already exist, which is an error
// because their secrets could not be used. With a key it checks the key opens an existing secret.
func CheckKey(ctx context.Context, st *store.Store, keyText string) (*secrets.Box, error) {
	id, cipher, found, err := st.AnySealedConnection(ctx)
	if err != nil {
		return nil, err
	}
	if keyText == "" {
		if found {
			return nil, errors.New("STUDIO_SECRETS_KEY is not set, but connections exist; set it to the key their secrets " +
				"were saved with (in studio/.env or the environment)")
		}
		return nil, nil
	}
	box, err := secrets.NewBox(keyText)
	if err != nil {
		return nil, err
	}
	if id != "" {
		if _, err := box.Open(cipher, id); err != nil {
			return nil, errors.New("STUDIO_SECRETS_KEY does not decrypt the secrets of existing connections; " +
				"it must be the key they were saved with")
		}
	}
	return box, nil
}

// Create stores a normalized connection, sealing its secrets (if any) to its new id.
func (s *Service) Create(ctx context.Context, in model.ConnectionInput) (model.Connection, error) {
	if s.box == nil {
		return model.Connection{}, errNoKey
	}
	id := auth.NewUUID()
	var sealed *store.SealedSecret
	if len(in.Secrets) > 0 {
		seal, err := s.seal(id, in.Secrets)
		if err != nil {
			return model.Connection{}, err
		}
		sealed = &seal
	}
	return s.store.CreateConnection(ctx, id, in, sealed)
}

// ReplaceSecrets swaps every secret of a connection for a normalized new set.
func (s *Service) ReplaceSecrets(ctx context.Context, id string, values map[string]string) (model.Connection, error) {
	if s.box == nil {
		return model.Connection{}, errNoKey
	}
	seal, err := s.seal(id, values)
	if err != nil {
		return model.Connection{}, err
	}
	return s.store.ReplaceConnectionSecret(ctx, id, seal)
}

func (s *Service) seal(id string, values map[string]string) (store.SealedSecret, error) {
	plain, err := json.Marshal(values)
	if err != nil {
		return store.SealedSecret{}, fmt.Errorf("encode secrets: %w", err)
	}
	cipher, err := s.box.Seal(plain, id)
	if err != nil {
		return store.SealedSecret{}, fmt.Errorf("seal secrets: %w", err)
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return store.SealedSecret{Cipher: cipher, Keys: keys}, nil
}

// Secrets opens a connection's secret fields. Only server-side clients use this; the API never returns them.
func (s *Service) Secrets(ctx context.Context, id string) (map[string]string, error) {
	if s.box == nil {
		return nil, errNoKey
	}
	cipher, err := s.store.ConnectionCipher(ctx, id)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	if len(cipher) == 0 {
		return out, nil
	}
	plain, err := s.box.Open(cipher, id)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(plain, &out); err != nil {
		return nil, fmt.Errorf("decode secrets: %w", err)
	}
	return out, nil
}
