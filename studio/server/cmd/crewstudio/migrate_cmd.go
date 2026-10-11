package main

import (
	"context"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/jackc/pgx/v5/pgxpool"

	"crewstudio/internal/db"
)

// migrationCommand is a one-off migration action chosen with a flag; the service does not start after it.
type migrationCommand struct {
	status    bool
	downTo    string // version to roll back to, as a number or file name; "" for none
	confirmed bool   // -yes: without it, a rollback only lists what it would undo
}

func (c migrationCommand) requested() bool { return c.status || c.downTo != "" }

func (c migrationCommand) validate() error {
	if c.status && c.downTo != "" {
		return fmt.Errorf("use -migrate-status or -migrate-down-to, not both")
	}
	if c.confirmed && c.downTo == "" {
		return fmt.Errorf("-yes only confirms -migrate-down-to")
	}
	if c.downTo != "" {
		if _, err := db.ParseVersion(c.downTo); err != nil {
			return fmt.Errorf("-migrate-down-to: %w", err)
		}
	}
	return nil
}

// run performs the command and writes what it did to out.
func (c migrationCommand) run(ctx context.Context, pool *pgxpool.Pool, out io.Writer) error {
	if c.status {
		return printMigrationStatus(ctx, pool, out)
	}
	version, err := db.ParseVersion(c.downTo)
	if err != nil {
		return fmt.Errorf("-migrate-down-to: %w", err)
	}
	plan, err := rollbackPlan(ctx, pool, version)
	if err != nil {
		return err
	}
	if len(plan) == 0 {
		fmt.Fprintf(out, "nothing to roll back: no applied migration is newer than version %d\n", version)
		return nil
	}
	fmt.Fprintln(out, "rolling back these migrations deletes the data they added:")
	for _, name := range plan {
		fmt.Fprintln(out, "  "+name)
	}
	if !c.confirmed {
		return fmt.Errorf("nothing rolled back: stop Crew Studio, back up the database, then add -yes to confirm")
	}
	rolledBack, err := db.MigrateDownTo(ctx, pool, version)
	for _, name := range rolledBack {
		fmt.Fprintln(out, "rolled back", name)
	}
	return err
}

// rollbackPlan lists the applied migrations newer than version, newest first: what rolling back to it undoes.
func rollbackPlan(ctx context.Context, pool *pgxpool.Pool, version int64) ([]string, error) {
	states, err := db.MigrationStatus(ctx, pool)
	if err != nil {
		return nil, err
	}
	var plan []string
	for i := len(states) - 1; i >= 0; i-- {
		if states[i].Applied && states[i].Version > version {
			plan = append(plan, states[i].Name)
		}
	}
	return plan, nil
}

func printMigrationStatus(ctx context.Context, pool *pgxpool.Pool, out io.Writer) error {
	states, err := db.MigrationStatus(ctx, pool)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "VERSION\tMIGRATION\tAPPLIED")
	for _, s := range states {
		applied := "pending"
		if s.Applied {
			applied = s.AppliedAt.Local().Format("2006-01-02 15:04:05")
		}
		fmt.Fprintf(w, "%d\t%s\t%s\n", s.Version, s.Name, applied)
	}
	return w.Flush()
}
