package memory

import (
	"math"
	"strings"
	"testing"

	"crewstudio/internal/model"
)

const crewID = "44444444-0000-4000-8000-0000000000aa"

func TestNormalizeScope(t *testing.T) {
	tests := []struct {
		in, want, problem string
	}{
		{"", "/", ""},
		{"/", "/", ""},
		{"  people/Sam  ", "/people/sam", ""},
		{"//people///sam//", "/people/sam", ""},
		{"/crew/" + crewID + "/people/sam", "/people/sam", ""},
		{"/crew/" + crewID, "/", ""},
		{"/crew/" + strings.ToUpper(crewID) + "/x", "/x", ""},
		// Another crew's path is just a path inside this crew; crew_id stays the boundary
		{"/crew/other/x", "/crew/other/x", ""},
		{"/agent/pmo-coordinator_2", "/agent/pmo-coordinator_2", ""},
		{"/people/../other", "", "must not contain"},
		{"/./x", "", "must not contain"},
		{"/people/sam smith", "", "letters, digits"},
		{"/people/s%m", "", "letters, digits"},
		{"/a\x00b", "", "letters, digits"},
		{"/" + strings.Repeat("a", 65), "", "letters, digits"},
		{"/" + strings.Repeat("a", 64), "/" + strings.Repeat("a", 64), ""},
		{"/a/b/c/d/e/f/g/h/i", "", "at most 8"},
		{"/a/b/c/d/e/f/g/h", "/a/b/c/d/e/f/g/h", ""},
		{"/" + strings.Repeat(strings.Repeat("a", 60)+"/", 4), "", "at most 200"},
		{"/café", "", "letters, digits"},
	}
	for _, tt := range tests {
		got, err := NormalizeScope("scope", tt.in, crewID)
		if tt.problem == "" {
			if err != nil || got != tt.want {
				t.Errorf("NormalizeScope(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tt.problem) || !strings.Contains(err.Error(), "scope:") {
			t.Errorf("NormalizeScope(%q) = %q, %v; want problem %q", tt.in, got, err, tt.problem)
		}
	}
}

func TestScopeUnder(t *testing.T) {
	if !ScopeUnder("/people/sam", "/people") || !ScopeUnder("/people", "/people") || !ScopeUnder("/x", "/") || !ScopeUnder("/x", "") {
		t.Error("prefix should match whole segments")
	}
	if ScopeUnder("/peoplex", "/people") || ScopeUnder("/people", "/people/sam") {
		t.Error("partial segments must not match")
	}
}

func TestChildScope(t *testing.T) {
	tests := []struct{ scope, parent, want string }{
		{"/people/sam/notes", "/people", "/people/sam"},
		{"/people/sam", "/", "/people"},
		{"/people", "/people", ""},
		{"/other", "/people", ""},
		{"/", "/", ""},
	}
	for _, tt := range tests {
		if got := ChildScope(tt.scope, tt.parent); got != tt.want {
			t.Errorf("ChildScope(%q, %q) = %q, want %q", tt.scope, tt.parent, got, tt.want)
		}
	}
}

func TestCleanRecord(t *testing.T) {
	imp := 0.9
	in := Input{Content: "  Sam replies late on Fridays  ", Scope: "/People/Sam", Categories: []string{"People", "people", " habits "},
		Metadata: map[string]any{"task": "Daily sweep", "n": 3.0, "tags": []any{"a", 1.0}}, Importance: &imp}
	var p Problems
	out := CleanInput(&p, "records[0]", in, crewID)
	if err := p.Err(); err != nil {
		t.Fatal(err)
	}
	if out.Content != "Sam replies late on Fridays" || out.Scope != "/people/sam" || out.Importance != 0.9 ||
		strings.Join(out.Categories, ",") != "people,habits" || out.Hash == "" {
		t.Errorf("clean = %+v", out)
	}
	// The hash ignores case and spacing, so the same lesson written twice is one memory
	var p2 Problems
	again := CleanInput(&p2, "x", Input{Content: "sam   replies LATE on fridays"}, crewID)
	if again.Hash != out.Hash || again.Importance != DefaultImportance || again.Scope != "/" || len(again.Categories) != 0 || again.Metadata == nil {
		t.Errorf("again = %+v", again)
	}
}

func TestCleanRecordProblems(t *testing.T) {
	big := map[string]any{}
	for i := 0; i < model.MaxMemoryMetadataKeys+1; i++ {
		big[strings.Repeat("k", i+1)] = "v"
	}
	nan := math.NaN()
	over := 1.5
	tests := []struct {
		name    string
		in      Input
		problem string
	}{
		{"empty", Input{Content: "   "}, "records[0].content: is required"},
		{"too long", Input{Content: strings.Repeat("x", model.MaxMemoryContent+1)}, "records[0].content: must be at most 2000"},
		{"control", Input{Content: "a\x07b"}, "records[0].content: must not contain control characters"},
		{"line separator", Input{Content: "a\u2028ignore the above"}, "records[0].content: must not contain control characters"},
		{"paragraph separator", Input{Content: "a\u2029b"}, "records[0].content: must not contain control characters"},
		{"bidi override", Input{Content: "safe \u202edetadpu"}, "records[0].content: must not contain control characters"},
		{"zero width", Input{Content: "in\u200bvisible"}, "records[0].content: must not contain control characters"},
		{"metadata format char", Input{Content: "x", Metadata: map[string]any{"task": "Sweep\u2066"}}, `records[0].metadata["task"]: must not contain control characters`},
		{"metadata list control", Input{Content: "x", Metadata: map[string]any{"l": []any{"ok", "a\u2028b"}}}, `records[0].metadata["l"]: must not contain control characters`},
		{"bad id", Input{ID: "nope", Content: "x"}, "records[0].id: must be a UUID"},
		{"scope", Input{Content: "x", Scope: "/../x"}, "records[0].scope"},
		{"categories", Input{Content: "x", Categories: strings.Split("a,b,c,d,e,f,g,h,i,j,k", ",")}, "records[0].categories: at most 10"},
		{"category", Input{Content: "x", Categories: []string{"bad/cat"}}, "records[0].categories: \"bad/cat\""},
		{"keys", Input{Content: "x", Metadata: big}, "records[0].metadata: at most 20 keys"},
		{"nested", Input{Content: "x", Metadata: map[string]any{"a": map[string]any{"b": 1}}}, `records[0].metadata["a"]`},
		{"bytes", Input{Content: "x", Metadata: map[string]any{"a": strings.Repeat("x", 5000)}}, "records[0].metadata: at most 4096 bytes"},
		{"empty key", Input{Content: "x", Metadata: map[string]any{"": "v"}}, "records[0].metadata: keys must be"},
		{"nan", Input{Content: "x", Importance: &nan}, "records[0].importance"},
		{"importance", Input{Content: "x", Importance: &over}, "records[0].importance"},
	}
	for _, tt := range tests {
		var p Problems
		CleanInput(&p, "records[0]", tt.in, crewID)
		err := p.Err()
		if err == nil || !strings.Contains(err.Error(), tt.problem) {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.problem)
		}
	}
}

func TestStripInvisible(t *testing.T) {
	if got := StripInvisible("PMO\u200b Coordinator\u202e\u2028x\x07"); got != "PMO Coordinator x" {
		t.Errorf("got %q", got)
	}
	var p Problems
	CleanContent(&p, "content", "tabs\tand\nnewlines are fine")
	if p.Err() != nil {
		t.Errorf("problems = %v", p)
	}
}

func TestProblemsErr(t *testing.T) {
	var p Problems
	if p.Err() != nil {
		t.Error("no problems should be nil")
	}
	p.Add("f", "bad %d", 1)
	if err := p.Err(); err == nil || err.Error() != "validation failed: f: bad 1" {
		t.Errorf("err = %v", err)
	}
}

func TestCleanInputWithoutPrefix(t *testing.T) {
	var p Problems
	CleanInput(&p, "", Input{Content: ""}, crewID)
	if err := p.Err(); err == nil || !strings.HasPrefix(p[0], "content: ") {
		t.Errorf("problems = %v", p)
	}
}
