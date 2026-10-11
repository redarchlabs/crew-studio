package model

import (
	"fmt"
	"strings"
	"testing"
)

const (
	folderA = "6a1f0000-0000-4000-8000-0000000000a1"
	folderB = "6a1f0000-0000-4000-8000-0000000000b2"
)

func TestKM2ConnectionSettings(t *testing.T) {
	in := ConnectionInput{Name: "KM2 Finance", Kind: KindKM2, AreaIDs: []string{"55555555-0000-4000-8000-000000000001"},
		Config: map[string]string{"base_url": "https://km2.example.com/", ConfigKM2DefaultFolder: strings.ToUpper(folderA),
			ConfigKM2WriteFolders: folderB + ", " + folderA + " " + folderB}}
	out, err := NormalizeConnection(in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Config["base_url"] != "https://km2.example.com" || out.Config[ConfigKM2DefaultFolder] != folderA ||
		out.Config[ConfigKM2WriteFolders] != folderB+","+folderA {
		t.Errorf("config = %+v", out.Config)
	}
	if got := KM2Folders(out.Config); len(got) != 2 || got[0] != folderA || got[1] != folderB {
		t.Errorf("folders = %v", got)
	}
	if got := KM2Folders(map[string]string{}); len(got) != 0 {
		t.Errorf("no folders = %v", got)
	}

	bad := in
	bad.Config = map[string]string{"base_url": "https://km2.example.com", ConfigKM2DefaultFolder: "nope", ConfigKM2WriteFolders: folderA + ",x"}
	_, err = NormalizeConnection(bad)
	if err == nil || !strings.Contains(err.Error(), "config.default_folder_id: must be a KM2 folder id") ||
		!strings.Contains(err.Error(), "config.write_folder_ids: \"x\" is not a KM2 folder id") {
		t.Errorf("bad ids: %v", err)
	}
	many := make([]string, MaxKM2WriteFolders+1)
	for i := range many {
		many[i] = fmt.Sprintf("6a1f0000-0000-4000-8000-%012x", i)
	}
	bad.Config = map[string]string{"base_url": "https://km2.example.com", ConfigKM2WriteFolders: strings.Join(many, ",")}
	if _, err := NormalizeConnection(bad); err == nil || !strings.Contains(err.Error(), "at most 50 folders") {
		t.Errorf("too many: %v", err)
	}
	if k, _ := ConnectionKindOf(KindKM2); !k.Testable {
		t.Error("km2 connections can be tested")
	}
	// Changing folders keeps the key; changing the base URL does not
	before := map[string]string{"base_url": "https://km2.example.com", ConfigKM2DefaultFolder: folderA}
	if DestinationChanged(KindKM2, before, map[string]string{"base_url": "https://km2.example.com", ConfigKM2DefaultFolder: folderB}) {
		t.Error("a new folder is not a new destination")
	}
	if !DestinationChanged(KindKM2, before, map[string]string{"base_url": "https://evil.example.com"}) {
		t.Error("a new base URL is a new destination")
	}
}

func TestNormalizeKM2Save(t *testing.T) {
	ok := KM2Save{Title: " Weekly status ", Content: "# Status", ExternalRef: " atlas-weekly-2026-W41 ", Folder: " Projects/Atlas ",
		Metadata: map[string]any{"project": "atlas", "week": 41.0, "final": true, "none": nil, "labels": []any{"a", 1.0}}, SourceFile: " reports/w41.md "}
	out, err := NormalizeKM2Save(ok)
	if err != nil {
		t.Fatal(err)
	}
	if out.Title != "Weekly status" || out.ExternalRef != "atlas-weekly-2026-W41" || out.Folder != "Projects/Atlas" ||
		out.SourceFile != "reports/w41.md" || out.Content != "# Status" {
		t.Errorf("out = %+v", out)
	}
	if ok.Title != " Weekly status " {
		t.Error("the input changed")
	}
	if KM2Subject(out.ExternalRef) != "km2:atlas-weekly-2026-W41" {
		t.Error("subject")
	}

	cases := map[string]KM2Save{
		"title: is required":                      {Content: "x", ExternalRef: "r"},
		"content: is required":                    {Title: "t", ExternalRef: "r"},
		"external_ref: is required":               {Title: "t", Content: "x"},
		"external_ref: use letters":               {Title: "t", Content: "x", ExternalRef: "-bad"},
		"content: is over":                        {Title: "t", Content: strings.Repeat("x", MaxKM2ContentBytes+1), ExternalRef: "r"},
		"content: must not contain NUL":           {Title: "t", Content: "a\x00b", ExternalRef: "r"},
		"title: must not contain control":         {Title: "a\x00b", Content: "x", ExternalRef: "r"},
		"title: must be at most 255":              {Title: strings.Repeat("t", 256), Content: "x", ExternalRef: "r"},
		"folder: must be at most":                 {Title: "t", Content: "x", ExternalRef: "r", Folder: strings.Repeat("f", 301)},
		"metadata: may not set reserved field(s)": {Title: "t", Content: "x", ExternalRef: "r", Metadata: map[string]any{"tags": "x", "access_keys": 1.0}},
		"metadata[\"deep\"]: must be a string":    {Title: "t", Content: "x", ExternalRef: "r", Metadata: map[string]any{"deep": map[string]any{"a": 1.0}}},
		"metadata[\"list\"]: must be a string":    {Title: "t", Content: "x", ExternalRef: "r", Metadata: map[string]any{"list": []any{[]any{1.0}}}},
		"source_file: must be at most":            {Title: "t", Content: "x", ExternalRef: "r", SourceFile: strings.Repeat("s", 301)},
	}
	for want, in := range cases {
		_, err := NormalizeKM2Save(in)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v", want, err)
		}
	}
}

func TestKM2MetadataLimits(t *testing.T) {
	big := map[string]any{}
	for i := 0; i < maxKM2MetadataKeys+1; i++ {
		big["k"+string(rune('A'+i%26))+string(rune('a'+i/26))] = "v"
	}
	if p := KM2MetadataProblems(big); len(p) == 0 || !strings.Contains(p[0], "at most 50 keys") {
		t.Errorf("keys: %v", p)
	}
	long := make([]any, maxKM2MetadataList+1)
	for i := range long {
		long[i] = "x"
	}
	if p := KM2MetadataProblems(map[string]any{"l": long}); len(p) == 0 || !strings.Contains(p[0], "at most 50 items") {
		t.Errorf("list: %v", p)
	}
	if p := KM2MetadataProblems(map[string]any{"s": strings.Repeat("x", maxKM2MetadataBytes)}); len(p) == 0 || !strings.Contains(p[0], "8192 bytes") {
		t.Errorf("bytes: %v", p)
	}
	if p := KM2MetadataProblems(map[string]any{"a\x00": "x", "b": "c\x00", "": "x"}); len(p) != 3 {
		t.Errorf("nul and empty: %v", p)
	}
	if p := KM2MetadataProblems(map[string]any{"n": 1, "i": int64(2), "f": float32(1.5)}); len(p) != 0 {
		t.Errorf("numbers: %v", p)
	}
	if p := KM2MetadataProblems(nil); len(p) != 0 {
		t.Errorf("nil: %v", p)
	}
}

func TestNormalizeKM2Reads(t *testing.T) {
	q, err := NormalizeKM2Query(KM2Query{Query: "  vendor contract  "})
	if err != nil || q.Query != "vendor contract" || q.TopK != DefaultKM2TopK {
		t.Errorf("query = %+v, %v", q, err)
	}
	for _, bad := range []KM2Query{{Query: " "}, {Query: "x", TopK: MaxKM2TopK + 1}, {Query: "x", TopK: -1}, {Query: strings.Repeat("q", MaxKM2QueryLen+1)}, {Query: "a\x00"}} {
		if _, err := NormalizeKM2Query(bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	a, err := NormalizeKM2Question(KM2Question{Question: " Who owns Atlas? "})
	if err != nil || a.Question != "Who owns Atlas?" {
		t.Errorf("question = %+v, %v", a, err)
	}
	if _, err := NormalizeKM2Question(KM2Question{}); err == nil {
		t.Error("empty question accepted")
	}
}

func TestAgentsCannotRecordKM2Saves(t *testing.T) {
	if err := CheckAgentLogRecord(LogRecord{Action: "digest:weekly", IdempotencyKey: "w41"}); err != nil {
		t.Errorf("ordinary entry: %v", err)
	}
	err := CheckAgentLogRecord(LogRecord{Action: ActionKM2Save, IdempotencyKey: "km2:run:abc"})
	if err == nil || len(err.(*ValidationError).Details) != 2 {
		t.Errorf("km2 entry: %v", err)
	}
}
