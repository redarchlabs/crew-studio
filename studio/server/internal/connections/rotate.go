package connections

import (
	"context"
	"errors"

	"crewstudio/internal/secrets"
	"crewstudio/internal/store"
)

// RotateKey re-encrypts every connection secret from oldKey to newKey in one transaction. It opens each secret
// with the old key (bound to its connection id, as when it was saved) and seals it with the new one. If any
// secret does not open, nothing changes. Key texts are never included in errors.
func RotateKey(ctx context.Context, st *store.Store, oldKey, newKey string) (int, error) {
	if oldKey == "" || newKey == "" {
		return 0, errors.New("both the old and the new key are needed")
	}
	oldBox, err := secrets.NewBox(oldKey)
	if err != nil {
		return 0, errors.New("the old key is not usable: " + err.Error())
	}
	newBox, err := secrets.NewBox(newKey)
	if err != nil {
		return 0, errors.New("the new key is not usable: " + err.Error())
	}
	return st.ResealConnectionSecrets(ctx, func(id string, sealed []byte) ([]byte, error) {
		plain, err := oldBox.Open(sealed, id)
		if err != nil {
			return nil, errors.New("the old key does not decrypt its secret; nothing was changed")
		}
		return newBox.Seal(plain, id)
	})
}
