package mcpserver

import (
	"slices"
	"testing"
)

func mustKind(t *testing.T, name string) kind {
	t.Helper()
	k, err := kindNamed(name)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// The hints, summaries and read-only lists are written by hand; this keeps them in step with the model types.
func TestKindTablesNameRealFields(t *testing.T) {
	for _, k := range kinds {
		fields := map[string]bool{}
		for _, f := range k.describe().Fields {
			fields[f.Name] = true
		}
		if !fields[k.key] {
			t.Errorf("%s: key field %q is not a field", k.name, k.key)
		}
		for _, list := range [][]string{k.summary, k.readOnly} {
			for _, name := range list {
				if !fields[name] {
					t.Errorf("%s: %q is not a field", k.name, name)
				}
			}
		}
		for name := range k.hints {
			if !fields[name] {
				t.Errorf("%s: hint for unknown field %q", k.name, name)
			}
		}
	}
}

func TestDescribeMarksTypesAndServerSetFields(t *testing.T) {
	info := mustKind(t, "crew").describe()
	if !info.Writable {
		t.Error("crews are writable")
	}
	byName := map[string]fieldInfo{}
	for _, f := range info.Fields {
		byName[f.Name] = f
	}
	checks := map[string]string{
		"name":           "string",
		"budget_usd":     "number",
		"manager_llm_id": "string or null",
		"agent_ids":      "array of string",
		"inputs":         "array of object {key, label, default, required}",
		"created_at":     "timestamp",
	}
	for name, typ := range checks {
		if byName[name].Type != typ {
			t.Errorf("%s type = %q, want %q", name, byName[name].Type, typ)
		}
	}
	if !byName["id"].ReadOnly || !byName["updated_at"].ReadOnly || byName["name"].ReadOnly {
		t.Errorf("read-only flags wrong: %+v", info.Fields)
	}
	if byName["process"].Hint == "" {
		t.Error("process should carry its allowed values")
	}

	sched := mustKind(t, "schedule").describe()
	for _, f := range sched.Fields {
		if f.Name == "target_type" || f.Name == "task_id" {
			t.Errorf("retired field %q is still described", f.Name)
		}
	}
}

func TestKindNames(t *testing.T) {
	all, writable := kindNames(false), kindNames(true)
	if !slices.Contains(all, "tool") || slices.Contains(writable, "tool") || slices.Contains(writable, "area") {
		t.Errorf("all = %v, writable = %v", all, writable)
	}
	if _, err := kindNamed("user"); err == nil {
		t.Error("user is not an MCP kind")
	}
}
