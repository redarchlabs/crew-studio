package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"crewstudio/internal/auth"
	"crewstudio/internal/store"
	"crewstudio/internal/testdb"
)

func TestSubcommandArgsResetAdmin(t *testing.T) {
	got := subcommandArgs([]string{"reset-admin-password", "-email", "a@x.test"})
	if !slices.Equal(got, []string{"-reset-admin-password", "-email", "a@x.test"}) {
		t.Errorf("subcommand = %v", got)
	}
}

func fixedPassword(p string) passwordSource { return func() (string, error) { return p, nil } }

// hashOf reads a user's stored hash and lockout state; only tests look at the hash.
func hashOf(t *testing.T, st *store.Store, email string) (hash string, failed int, locked bool) {
	t.Helper()
	err := st.Pool().QueryRow(context.Background(),
		`SELECT coalesce(password_hash, ''), failed_logins, locked_until IS NOT NULL FROM users WHERE email = $1`, email).
		Scan(&hash, &failed, &locked)
	if err != nil {
		t.Fatal(err)
	}
	return hash, failed, locked
}

func TestResetAdminPassword(t *testing.T) {
	ctx := context.Background()
	st := store.New(testdb.New(t, "cmd_reset_admin_test"))
	oldHash, err := auth.HashPassword("original-password-1")
	if err != nil {
		t.Fatal(err)
	}
	adminID, err := st.CreateFirstAdmin(ctx, "boss@x.test", "Boss", oldHash)
	if err != nil {
		t.Fatal(err)
	}
	// Locked out after too many tries, with a session still open
	if _, err := st.Pool().Exec(ctx, `UPDATE users SET failed_logins = 9, locked_until = now() + interval '1 hour' WHERE id = $1`, adminID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSession(ctx, adminID, "local", "127.0.0.1", "test"); err != nil {
		t.Fatal(err)
	}

	t.Run("too short is refused and changes nothing", func(t *testing.T) {
		var out bytes.Buffer
		err := resetAdminPassword(ctx, st, "", fixedPassword("short"), true, &out)
		if err == nil || !strings.Contains(err.Error(), "at least") {
			t.Fatalf("err = %v", err)
		}
		if h, _, _ := hashOf(t, st, "boss@x.test"); h != oldHash {
			t.Error("hash changed")
		}
	})

	t.Run("reading the password fails", func(t *testing.T) {
		err := resetAdminPassword(ctx, st, "", func() (string, error) { return "", errors.New("no match") }, true, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "no match") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("the only admin needs no email", func(t *testing.T) {
		var out bytes.Buffer
		if err := resetAdminPassword(ctx, st, "", fixedPassword("brand-new-password-2"), false, &out); err != nil {
			t.Fatal(err)
		}
		h, failed, locked := hashOf(t, st, "boss@x.test")
		if !auth.CheckPassword(h, "brand-new-password-2") || auth.CheckPassword(h, "original-password-1") {
			t.Error("new password not set")
		}
		if failed != 0 || locked {
			t.Errorf("lockout not cleared: failed=%d locked=%v", failed, locked)
		}
		text := out.String()
		if !strings.Contains(text, "boss@x.test") || !strings.Contains(text, "1 session(s)") || !strings.Contains(text, "STUDIO_LOCAL_LOGIN") {
			t.Errorf("output = %q", text)
		}
		if strings.Contains(text, "brand-new-password-2") {
			t.Error("password printed")
		}
	})

	// A second administrator, and a disabled one
	hash2, _ := auth.HashPassword("second-admin-pass-3")
	second, err := st.Pool().Exec(ctx, `INSERT INTO users (kind, email, name, all_areas, password_hash) VALUES ('human', 'two@x.test', 'Two', true, $1)`, hash2)
	if err != nil || second.RowsAffected() != 1 {
		t.Fatal(err)
	}
	if _, err := st.Pool().Exec(ctx, `INSERT INTO users (kind, email, name, all_areas, status) VALUES ('human', 'off@x.test', 'Off', true, 'disabled')`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Pool().Exec(ctx, `INSERT INTO user_roles (user_id, role_id) SELECT id, $1 FROM users WHERE email IN ('two@x.test', 'off@x.test')`, auth.AdministratorRoleID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Pool().Exec(ctx, `INSERT INTO users (kind, email, name, all_areas) VALUES ('human', 'plain@x.test', 'Plain', true)`); err != nil {
		t.Fatal(err)
	}

	for name, tc := range map[string]struct{ email, want string }{
		"two admins need an email": {"", "-email"},
		"unknown email":            {"nobody@x.test", "no administrator"},
		"a non-administrator":      {"plain@x.test", "no administrator"},
		"a disabled administrator": {"off@x.test", "disabled"},
	} {
		t.Run(name, func(t *testing.T) {
			err := resetAdminPassword(ctx, st, tc.email, fixedPassword("whatever-long-pass-4"), true, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}

	t.Run("email picks one, ignoring case", func(t *testing.T) {
		var out bytes.Buffer
		if err := resetAdminPassword(ctx, st, " TWO@x.test ", fixedPassword("picked-password-5"), true, &out); err != nil {
			t.Fatal(err)
		}
		if h, _, _ := hashOf(t, st, "two@x.test"); !auth.CheckPassword(h, "picked-password-5") {
			t.Error("second admin not reset")
		}
		if h, _, _ := hashOf(t, st, "boss@x.test"); !auth.CheckPassword(h, "brand-new-password-2") {
			t.Error("first admin changed")
		}
		if strings.Contains(out.String(), "STUDIO_LOCAL_LOGIN") {
			t.Error("local-login note shown while it is on")
		}
	})
}

func TestResetAdminPasswordNoAdmins(t *testing.T) {
	st := store.New(testdb.New(t, "cmd_reset_admin_empty_test"))
	err := resetAdminPassword(context.Background(), st, "", fixedPassword("long-enough-pass-6"), true, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "/setup") {
		t.Fatalf("err = %v", err)
	}
}

func TestTerminalPasswordFromPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString("piped-password-7\r\nignored second line\n"); err != nil {
		t.Fatal(err)
	}
	w.Close()
	var prompt bytes.Buffer
	got, err := terminalPassword(r, &prompt)()
	if err != nil || got != "piped-password-7" {
		t.Fatalf("got %q, %v", got, err)
	}
	if prompt.Len() != 0 {
		t.Errorf("prompted on a pipe: %q", prompt.String())
	}
}
