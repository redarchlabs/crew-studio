package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

const (
	bcryptCost        = 12
	MinPasswordLength = 12
	// bcrypt only reads the first 72 bytes, so longer passwords are refused rather than silently cut
	MaxPasswordBytes = 72

	APIKeyPrefix  = "csk"
	TriggerPrefix = "cst"
)

// RandomToken returns n random bytes, base64url encoded.
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// HashToken is how session tokens, API keys and trigger tokens are stored: they are long and random,
// so a fast hash is enough.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// TokensEqual compares two hashes in constant time.
func TokensEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// NewKey creates a key of the form <kind>_<prefix>_<secret>. The prefix is stored in clear to find the row;
// the whole key is hashed.
func NewKey(kind string) (key, prefix string) {
	prefix = strings.ToLower(RandomToken(6))
	prefix = strings.NewReplacer("-", "x", "_", "y").Replace(prefix)
	return fmt.Sprintf("%s_%s_%s", kind, prefix, RandomToken(32)), prefix
}

// SplitKey returns the prefix of a key made by NewKey, or false if it is not shaped like one.
func SplitKey(kind, key string) (string, bool) {
	rest, ok := strings.CutPrefix(key, kind+"_")
	if !ok {
		return "", false
	}
	prefix, secret, ok := strings.Cut(rest, "_")
	if !ok || len(prefix) != 8 || len(secret) < 40 {
		return "", false
	}
	return prefix, true
}

// CheckPasswordPolicy returns a problem with the password, or "".
func CheckPasswordPolicy(password string) string {
	switch {
	case utf8.RuneCountInString(password) < MinPasswordLength:
		return fmt.Sprintf("password: must be at least %d characters", MinPasswordLength)
	case len(password) > MaxPasswordBytes:
		return fmt.Sprintf("password: must be at most %d bytes", MaxPasswordBytes)
	case strings.TrimSpace(password) == "":
		return "password: must not be blank"
	}
	return ""
}

func HashPassword(password string) (string, error) {
	if problem := CheckPasswordPolicy(password); problem != "" {
		return "", errors.New(problem)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// dummyHash is compared against when the account does not exist, so the response takes as long either way.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("crew-studio-timing-equaliser"), bcryptCost)

// CheckPassword reports whether password matches hash. An empty hash never matches.
func CheckPassword(hash, password string) bool {
	if hash == "" {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// NewUUID returns a random (version 4) UUID string.
func NewUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
