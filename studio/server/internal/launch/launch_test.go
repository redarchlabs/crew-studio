package launch

import (
	"strings"
	"testing"

	"crewstudio/internal/model"
)

func TestSafeFilename(t *testing.T) {
	for in, want := range map[string]string{
		"report.pdf": "report.pdf", "../../etc/passwd": "passwd", `C:\Users\me\shot.png`: "shot.png",
		"..": "file", "  .hidden  ": "hidden", "naïve résumé.docx": "na_ve r_sum_.docx", "a/b/": "b",
	} {
		if got := SafeFilename(in); got != want {
			t.Errorf("SafeFilename(%q) = %q, want %q", in, got, want)
		}
	}
	long := SafeFilename(strings.Repeat("x", 300) + ".png")
	if len(long) != 120 || !strings.HasSuffix(long, ".png") {
		t.Errorf("long name = %q", long)
	}
}

func TestWorkOrderSpecGivesUniquePaths(t *testing.T) {
	spec := workOrderSpec(model.WorkOrder{ID: "w", Number: 7, Title: "T", Brief: "B"}, []model.Attachment{
		{ID: "1", Filename: "shot.png"}, {ID: "2", Filename: "SHOT.png"}, {ID: "3", Filename: "notes"},
	})
	paths := []string{spec.Attachments[0].Path, spec.Attachments[1].Path, spec.Attachments[2].Path}
	if paths[0] != "attachments/shot.png" || paths[1] != "attachments/SHOT-2.png" || paths[2] != "attachments/notes" || spec.Number != 7 {
		t.Errorf("paths = %v", paths)
	}
}
