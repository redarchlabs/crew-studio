package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"crewstudio/internal/auth"
	"crewstudio/internal/store"
)

// resetAdminSubcommand is the command form (crewstudio reset-admin-password); it is the same as the flag
const resetAdminSubcommand = "reset-admin-password"

// adminResetStore is what resetting an administrator's password needs from the store.
type adminResetStore interface {
	Administrators(ctx context.Context) ([]store.Administrator, error)
	SetPassword(ctx context.Context, id string, hash *string) error
	DeleteUserSessions(ctx context.Context, userID string) (int64, error)
}

// passwordSource asks for the new password: twice without echo at a terminal, once from a pipe.
type passwordSource func() (password string, err error)

// resetAdminPassword sets a new local password for an administrator, clears any lockout and signs them out
// everywhere. email picks the administrator; it may be empty when there is exactly one. The password is never
// printed or logged.
func resetAdminPassword(ctx context.Context, st adminResetStore, email string, readPassword passwordSource, localLogin bool, out io.Writer) error {
	admin, err := pickAdministrator(ctx, st, email)
	if err != nil {
		return fmt.Errorf("%s: %w", resetAdminSubcommand, err)
	}
	password, err := readPassword()
	if err != nil {
		return fmt.Errorf("%s: %w", resetAdminSubcommand, err)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("%s: %w", resetAdminSubcommand, err)
	}
	if err := st.SetPassword(ctx, admin.ID, &hash); err != nil {
		return fmt.Errorf("%s: %w", resetAdminSubcommand, err)
	}
	ended, err := st.DeleteUserSessions(ctx, admin.ID)
	if err != nil {
		return fmt.Errorf("%s: password changed, but signing out old sessions failed: %w", resetAdminSubcommand, err)
	}
	fmt.Fprintf(out, "Password reset for %s. Any lockout is cleared and %d session(s) were signed out.\n", admin.Email, ended)
	if !localLogin {
		fmt.Fprintln(out, "Note: password sign-in is off (STUDIO_LOCAL_LOGIN=false), so this password works only after you turn it back on.")
	}
	return nil
}

// pickAdministrator finds the active administrator to reset: the one named by email, or the only one there is.
func pickAdministrator(ctx context.Context, st adminResetStore, email string) (store.Administrator, error) {
	admins, err := st.Administrators(ctx)
	if err != nil {
		return store.Administrator{}, err
	}
	email = strings.TrimSpace(email)
	if email == "" {
		active := activeAdministrators(admins)
		switch len(active) {
		case 0:
			return store.Administrator{}, errors.New("there is no active administrator; start Crew Studio and use the /setup link it prints")
		case 1:
			return active[0], nil
		default:
			return store.Administrator{}, fmt.Errorf("there are %d administrators; choose one with -email (%s)", len(active), adminEmails(active))
		}
	}
	for _, a := range admins {
		if strings.EqualFold(a.Email, email) {
			if a.Status != "active" {
				return store.Administrator{}, fmt.Errorf("%s is %s; another administrator must turn the account back on", a.Email, a.Status)
			}
			return a, nil
		}
	}
	return store.Administrator{}, fmt.Errorf("no administrator has the email %q", email)
}

func activeAdministrators(admins []store.Administrator) []store.Administrator {
	var out []store.Administrator
	for _, a := range admins {
		if a.Status == "active" {
			out = append(out, a)
		}
	}
	return out
}

func adminEmails(admins []store.Administrator) string {
	emails := make([]string, 0, len(admins))
	for _, a := range admins {
		emails = append(emails, a.Email)
	}
	return strings.Join(emails, ", ")
}

// terminalPassword asks for the password twice without echoing it. When stdin is not a terminal (a pipe), it reads
// one line instead, so the command can be scripted without the password appearing in the command line.
func terminalPassword(in *os.File, prompt io.Writer) passwordSource {
	return func() (string, error) {
		fd := int(in.Fd())
		if !term.IsTerminal(fd) {
			line, err := bufio.NewReader(in).ReadString('\n')
			if err != nil && !errors.Is(err, io.EOF) {
				return "", fmt.Errorf("read password: %w", err)
			}
			return strings.TrimRight(line, "\r\n"), nil
		}
		first, err := readHidden(fd, prompt, "New password: ")
		if err != nil {
			return "", err
		}
		if problem := auth.CheckPasswordPolicy(first); problem != "" {
			return "", errors.New(problem)
		}
		second, err := readHidden(fd, prompt, "Type it again: ")
		if err != nil {
			return "", err
		}
		if first != second {
			return "", errors.New("the two passwords do not match; nothing was changed")
		}
		return first, nil
	}
}

func readHidden(fd int, prompt io.Writer, label string) (string, error) {
	fmt.Fprint(prompt, label)
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(prompt)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return string(b), nil
}
