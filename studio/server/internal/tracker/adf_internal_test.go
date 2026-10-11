package tracker

import (
	"encoding/json"
	"testing"
)

func TestADFText(t *testing.T) {
	raw := `{"type":"doc","version":1,"content":[
		{"type":"paragraph","content":[{"type":"mention","attrs":{"id":"a","text":"@Sam"}},{"type":"text","text":" please look"},{"type":"hardBreak"},{"type":"text","text":"line two"}]},
		{"type":"paragraph","content":[]},
		{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"emoji","attrs":{"shortName":":ok:"}},{"type":"inlineCard","attrs":{"url":"https://x.example"}}]}]}]}
	]}`
	var n adfNode
	if err := json.Unmarshal([]byte(raw), &n); err != nil {
		t.Fatal(err)
	}
	want := "@Sam please look\nline two\n\n:ok:https://x.example"
	if got := adfText(n); got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
}

func TestADFDocKeepsTextAsText(t *testing.T) {
	doc := adfDoc("first\r\nsecond\n\n\n\nthird", []Person{{AccountID: "a1"}})
	data, _ := json.Marshal(doc)
	want := `{"content":[{"content":[{"attrs":{"id":"a1","text":"@user"},"type":"mention"},{"text":" ","type":"text"},{"text":"first","type":"text"},{"type":"hardBreak"},{"text":"second","type":"text"}],"type":"paragraph"},{"content":[{"text":"third","type":"text"}],"type":"paragraph"}],"type":"doc","version":1}`
	if string(data) != want {
		t.Errorf("doc = %s", data)
	}
	only := adfDoc("", []Person{{AccountID: "a1", Name: "Sam"}})
	if content := only["content"].([]any); len(content) != 1 {
		t.Errorf("mentions only = %v", only)
	}
}

func TestOneLineAndCategories(t *testing.T) {
	if oneLine("a\n  b\tc", 3) != "a b…" || oneLine("ab", 5) != "ab" {
		t.Error("oneLine")
	}
	if jiraCategory("weird", "x") != "to_do" || jiraCategory("done", "Cancelled") != "canceled" || jiraCategory("indeterminate", "x") != "in_progress" {
		t.Error("jira categories")
	}
	if linearCategory("backlog") != "to_do" || linearCategory("duplicate") != "canceled" || linearCategory("completed") != "done" {
		t.Error("linear categories")
	}
}
