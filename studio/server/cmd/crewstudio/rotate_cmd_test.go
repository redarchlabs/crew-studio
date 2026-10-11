package main

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"crewstudio/internal/connections"
	"crewstudio/internal/model"
	"crewstudio/internal/secrets"
	"crewstudio/internal/store"
	"crewstudio/internal/testdb"
)

func TestSubcommandArgs(t *testing.T) {
	if got := subcommandArgs([]string{"rotate-secrets-key", "-studio-dir", "x"}); !slices.Equal(got, []string{"-rotate-secrets-key", "-studio-dir", "x"}) {
		t.Errorf("subcommand = %v", got)
	}
	if got := subcommandArgs([]string{"-migrate"}); !slices.Equal(got, []string{"-migrate"}) {
		t.Errorf("flags = %v", got)
	}
	if got := subcommandArgs(nil); len(got) != 0 {
		t.Errorf("none = %v", got)
	}
}

func env(values map[string]string) func(string) string {
	return func(k string) string { return values[k] }
}

func TestRotateSecretsKey(t *testing.T) {
	ctx := context.Background()
	st := store.New(testdb.New(t, "cmd_rotate_test"))
	oldKey, newKey := secrets.GenerateKey(), secrets.GenerateKey()
	oldBox, err := secrets.NewBox(oldKey)
	if err != nil {
		t.Fatal(err)
	}
	svc := connections.New(st, oldBox)
	var ids []string
	for _, name := range []string{"one", "two"} {
		c, err := svc.Create(ctx, model.ConnectionInput{Name: name, Kind: "http", Config: map[string]string{"base_url": "https://x.example"},
			Secrets: map[string]string{"auth_value": "secret-" + name}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, c.ID)
	}
	if _, err := svc.Create(ctx, model.ConnectionInput{Name: "no-secret", Kind: "http", Config: map[string]string{"base_url": "https://x.example"}}); err != nil {
		t.Fatal(err)
	}

	for name, tc := range map[string]struct {
		current string
		vars    map[string]string
		want    string
	}{
		"no old key": {"", map[string]string{newKeyEnvVar: newKey}, oldKeyEnvVar},
		"no new key": {oldKey, nil, newKeyEnvVar},
		"blank new":  {oldKey, map[string]string{newKeyEnvVar: "   "}, newKeyEnvVar},
		"same key":   {oldKey, map[string]string{newKeyEnvVar: oldKey}, "same as the old one"},
		"bad new":    {oldKey, map[string]string{newKeyEnvVar: "short"}, "new key is not usable"},
		"bad old":    {"", map[string]string{oldKeyEnvVar: "short", newKeyEnvVar: newKey}, "old key is not usable"},
		"wrong old":  {secrets.GenerateKey(), map[string]string{newKeyEnvVar: newKey}, "does not decrypt"},
	} {
		var out bytes.Buffer
		err := rotateSecretsKey(ctx, st, tc.current, env(tc.vars), &out)
		if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), newKey) || strings.Contains(err.Error(), oldKey) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// Nothing changed after the failures
	if got, err := svc.Secrets(ctx, ids[0]); err != nil || got["auth_value"] != "secret-one" {
		t.Fatalf("old key no longer opens: %v %v", got, err)
	}

	var out bytes.Buffer
	if err := rotateSecretsKey(ctx, st, "", env(map[string]string{oldKeyEnvVar: oldKey, newKeyEnvVar: newKey}), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "re-encrypted 2 connection secret(s)") || strings.Contains(out.String(), newKey) {
		t.Errorf("output = %q", out.String())
	}
	newBox, _ := secrets.NewBox(newKey)
	rotated := connections.New(st, newBox)
	for i, id := range ids {
		got, err := rotated.Secrets(ctx, id)
		if err != nil || got["auth_value"] != []string{"secret-one", "secret-two"}[i] {
			t.Errorf("after rotation %s: %v %v", id, got, err)
		}
		if _, err := svc.Secrets(ctx, id); err == nil {
			t.Errorf("the old key still opens %s", id)
		}
	}
	if _, err := connections.CheckKey(ctx, st, newKey); err != nil {
		t.Errorf("startup check with the new key: %v", err)
	}

	// A half-rotated database (one secret under another key) is refused as a whole
	other, _ := secrets.NewBox(secrets.GenerateKey())
	if _, err := connections.New(st, other).ReplaceSecrets(ctx, ids[1], map[string]string{"auth_value": "x"}); err != nil {
		t.Fatal(err)
	}
	third := secrets.GenerateKey()
	if err := rotateSecretsKey(ctx, st, newKey, env(map[string]string{newKeyEnvVar: third}), &out); err == nil {
		t.Fatal("rotation with an unreadable secret succeeded")
	}
	if got, err := rotated.Secrets(ctx, ids[0]); err != nil || got["auth_value"] != "secret-one" {
		t.Errorf("a failed rotation changed a secret: %v %v", got, err)
	}
}
