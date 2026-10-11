package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"crewstudio/internal/connections"
	"crewstudio/internal/store"
)

const (
	// rotateSubcommand is the command form (crewstudio rotate-secrets-key); it is the same as the flag
	rotateSubcommand = "rotate-secrets-key"
	oldKeyEnvVar     = "STUDIO_SECRETS_KEY_OLD"
	newKeyEnvVar     = "STUDIO_SECRETS_KEY_NEW"
)

// subcommandArgs turns "rotate-secrets-key [flags]" or "reset-admin-password [flags]" into the flag form, so both
// forms work.
func subcommandArgs(args []string) []string {
	if len(args) > 0 && (args[0] == rotateSubcommand || args[0] == resetAdminSubcommand) {
		return append([]string{"-" + args[0]}, args[1:]...)
	}
	return args
}

// rotateSecretsKey re-encrypts every connection secret from the old key to the new one, in one transaction.
// The old key is STUDIO_SECRETS_KEY_OLD, or STUDIO_SECRETS_KEY when that is not set; the new key is
// STUDIO_SECRETS_KEY_NEW. Neither is ever printed. Stop Crew Studio first, so no secret is saved meanwhile under the
// old key.
func rotateSecretsKey(ctx context.Context, st *store.Store, currentKey string, getenv func(string) string, out io.Writer) error {
	oldKey := strings.TrimSpace(getenv(oldKeyEnvVar))
	if oldKey == "" {
		oldKey = strings.TrimSpace(currentKey)
	}
	newKey := strings.TrimSpace(getenv(newKeyEnvVar))
	switch {
	case oldKey == "":
		return fmt.Errorf("%s: set %s (or STUDIO_SECRETS_KEY) to the key the secrets were saved with", rotateSubcommand, oldKeyEnvVar)
	case newKey == "":
		return fmt.Errorf("%s: set %s to the new key (for example: openssl rand -base64 32)", rotateSubcommand, newKeyEnvVar)
	case oldKey == newKey:
		return errors.New(rotateSubcommand + ": the new key is the same as the old one")
	}
	n, err := connections.RotateKey(ctx, st, oldKey, newKey)
	if err != nil {
		return fmt.Errorf("%s: %w", rotateSubcommand, err)
	}
	fmt.Fprintf(out, "re-encrypted %d connection secret(s) with the new key.\n", n)
	fmt.Fprintln(out, "Now set STUDIO_SECRETS_KEY to the new key (and remove "+oldKeyEnvVar+" and "+newKeyEnvVar+"), then start Crew Studio.")
	return nil
}
