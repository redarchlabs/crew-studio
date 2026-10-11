// Package secrets encrypts credentials kept in the database (connection secrets) with AES-256-GCM.
// The key comes from STUDIO_SECRETS_KEY and never leaves the server process.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// KeyBytes is the AES-256 key size.
const KeyBytes = 32

// version is the first byte of every sealed value, so a later format (or key id) can be told apart.
const version byte = 1

// ErrDecrypt means a sealed value could not be opened: wrong key, wrong owner, or damaged data.
var ErrDecrypt = errors.New("could not decrypt the secret (wrong STUDIO_SECRETS_KEY, or the data is damaged)")

// Box seals and opens secrets with one key. It is safe for concurrent use.
type Box struct {
	aead cipher.AEAD
}

// NewBox parses a 32-byte key written as base64 (standard or URL-safe, padded or not) or 64 hex characters.
// Errors never repeat the key text.
func NewBox(keyText string) (*Box, error) {
	key, err := parseKey(strings.TrimSpace(keyText))
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("STUDIO_SECRETS_KEY: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("STUDIO_SECRETS_KEY: %w", err)
	}
	return &Box{aead: aead}, nil
}

func parseKey(text string) ([]byte, error) {
	if text == "" {
		return nil, errors.New("STUDIO_SECRETS_KEY is empty")
	}
	if len(text) == hex.EncodedLen(KeyBytes) {
		if key, err := hex.DecodeString(text); err == nil {
			return key, nil
		}
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if key, err := enc.DecodeString(text); err == nil && len(key) == KeyBytes {
			return key, nil
		}
	}
	return nil, fmt.Errorf("STUDIO_SECRETS_KEY must be %d random bytes written as base64 or %d hex characters "+
		"(for example: openssl rand -base64 32)", KeyBytes, hex.EncodedLen(KeyBytes))
}

// GenerateKey returns a new random key in the base64 form NewBox accepts.
func GenerateKey() string {
	key := make([]byte, KeyBytes)
	if _, err := rand.Read(key); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return base64.StdEncoding.EncodeToString(key)
}

// Seal encrypts plaintext. owner (such as a connection id) is bound to the result as additional data, so a
// sealed value copied onto another row does not open there. The output is version | nonce | ciphertext.
func (b *Box) Seal(plaintext []byte, owner string) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("make nonce: %w", err)
	}
	out := make([]byte, 0, 1+len(nonce)+len(plaintext)+b.aead.Overhead())
	out = append(out, version)
	out = append(out, nonce...)
	return b.aead.Seal(out, nonce, plaintext, []byte(owner)), nil
}

// Open decrypts a value made by Seal for the same owner.
func (b *Box) Open(sealed []byte, owner string) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(sealed) < 1+n+b.aead.Overhead() || sealed[0] != version {
		return nil, ErrDecrypt
	}
	plain, err := b.aead.Open(nil, sealed[1:1+n], sealed[1+n:], []byte(owner))
	if err != nil {
		return nil, ErrDecrypt
	}
	return plain, nil
}
