package main

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"crewstudio/internal/db"
	"crewstudio/internal/testdb"
)

func TestMigrationCommandValidate(t *testing.T) {
	tests := []struct {
		name    string
		cmd     migrationCommand
		wantErr string
	}{
		{"nothing asked", migrationCommand{}, ""},
		{"status", migrationCommand{status: true}, ""},
		{"down by number", migrationCommand{downTo: "6"}, ""},
		{"down by file name", migrationCommand{downTo: "0006_llm_vision"}, ""},
		{"both", migrationCommand{status: true, downTo: "6"}, "not both"},
		{"bad version", migrationCommand{downTo: "latest"}, "not a migration version"},
		{"confirmed down", migrationCommand{downTo: "6", confirmed: true}, ""},
		{"yes alone", migrationCommand{confirmed: true}, "only confirms"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cmd.validate()
			if tt.wantErr == "" && err != nil {
				t.Errorf("unexpected error %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Errorf("err = %v, want one containing %q", err, tt.wantErr)
			}
		})
	}
	if (migrationCommand{}).requested() || !(migrationCommand{status: true}).requested() || !(migrationCommand{downTo: "0"}).requested() {
		t.Error("requested() is wrong")
	}
}

func TestMigrationCommandStatusAndDownTo(t *testing.T) {
	pool := testdb.New(t, "cmd_migrate_test")
	ctx := context.Background()
	appliedNames := func(t *testing.T, ctx context.Context) []string {
		t.Helper()
		states, err := db.MigrationStatus(ctx, pool)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, s := range states {
			if s.Applied {
				names = append(names, s.Name)
			}
		}
		return names
	}

	var out bytes.Buffer
	if err := (migrationCommand{status: true}).run(ctx, pool, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "0007_agent_library") || strings.Contains(out.String(), "pending") {
		t.Errorf("status of a fully migrated database:\n%s", out.String())
	}

	// Without -yes it only lists what it would undo
	out.Reset()
	err := (migrationCommand{downTo: "0006_llm_vision"}).run(ctx, pool, &out)
	if err == nil || !strings.Contains(err.Error(), "-yes") || !strings.Contains(out.String(), "0007_agent_library") {
		t.Fatalf("unconfirmed rollback: err %v, output %q", err, out.String())
	}
	if !slices.Contains(appliedNames(t, ctx), "0007_agent_library") {
		t.Fatal("unconfirmed rollback changed the database")
	}

	out.Reset()
	if err := (migrationCommand{downTo: "0006_llm_vision", confirmed: true}).run(ctx, pool, &out); err != nil {
		t.Fatal(err)
	}
	// Newest first, so 0007 is the last one rolled back
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if lines[len(lines)-1] != "rolled back 0007_agent_library" {
		t.Errorf("down-to output = %q", out.String())
	}

	out.Reset()
	if err := (migrationCommand{downTo: "6", confirmed: true}).run(ctx, pool, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "nothing to roll back") {
		t.Errorf("second down-to output = %q", out.String())
	}

	out.Reset()
	if err := (migrationCommand{status: true}).run(ctx, pool, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "pending") {
		t.Errorf("rolled-back migration not shown as pending:\n%s", out.String())
	}
	if _, err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
}
