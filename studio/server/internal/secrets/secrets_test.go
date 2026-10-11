package secrets

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func testKey(fill byte) string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{fill}, KeyBytes))
}

func TestParseKeyFormats(t *testing.T) {
	raw := bytes.Repeat([]byte{7}, KeyBytes)
	for name, text := range map[string]string{
		"base64":     base64.StdEncoding.EncodeToString(raw),
		"base64url":  base64.RawURLEncoding.EncodeToString(raw),
		"hex":        hex.EncodeToString(raw),
		"whitespace": "  " + hex.EncodeToString(raw) + "\n",
	} {
		if _, err := NewBox(text); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, text := range map[string]string{
		"empty":     "",
		"short":     base64.StdEncoding.EncodeToString(raw[:16]),
		"long hex":  hex.EncodeToString(append(raw, 1)),
		"not a key": "correct horse battery staple",
	} {
		if _, err := NewBox(text); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	box, err := NewBox(testKey(1))
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte(`{"api_token":"s3cret"}`)
	sealed, err := box.Seal(plain, "conn-1")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("s3cret")) {
		t.Fatal("sealed bytes contain the plaintext")
	}
	again, _ := box.Seal(plain, "conn-1")
	if bytes.Equal(sealed, again) {
		t.Error("two seals of the same text must differ (random nonce)")
	}
	got, err := box.Open(sealed, "conn-1")
	if err != nil || string(got) != string(plain) {
		t.Fatalf("open = %q, %v", got, err)
	}
}

func TestOpenRejectsTamperingWrongKeyAndWrongContext(t *testing.T) {
	box, _ := NewBox(testKey(1))
	other, _ := NewBox(testKey(2))
	sealed, _ := box.Seal([]byte("hello"), "conn-1")

	if _, err := other.Open(sealed, "conn-1"); !errors.Is(err, ErrDecrypt) {
		t.Errorf("wrong key: err = %v", err)
	}
	// A secret copied onto another connection's row must not decrypt there
	if _, err := box.Open(sealed, "conn-2"); !errors.Is(err, ErrDecrypt) {
		t.Errorf("wrong context: err = %v", err)
	}
	tampered := append([]byte{}, sealed...)
	tampered[len(tampered)-1] ^= 0xff
	if _, err := box.Open(tampered, "conn-1"); !errors.Is(err, ErrDecrypt) {
		t.Errorf("tampered: err = %v", err)
	}
	for _, bad := range [][]byte{nil, {}, {version}, {9, 1, 2, 3}} {
		if _, err := box.Open(bad, "conn-1"); !errors.Is(err, ErrDecrypt) {
			t.Errorf("%v: err = %v", bad, err)
		}
	}
}

func TestErrorsNeverIncludeKeyMaterial(t *testing.T) {
	key := "not-valid-but-secret-looking-key-material"
	_, err := NewBox(key)
	if err == nil || strings.Contains(err.Error(), key) {
		t.Errorf("err = %v", err)
	}
}

func TestGenerateKeyMakesAUsableKey(t *testing.T) {
	key := GenerateKey()
	if _, err := NewBox(key); err != nil {
		t.Fatalf("generated key %v", err)
	}
	if GenerateKey() == key {
		t.Error("keys must be random")
	}
}
