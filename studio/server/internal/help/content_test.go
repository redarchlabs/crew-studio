package help

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// publicArticles are readable by everyone signed in; keep them free of anything a permission should hide.
var publicArticles = map[string]bool{"getting-started": true, "access-and-areas": true, "api-explorer": true}

var anyHelpLink = regexp.MustCompile(`\]\(/help/([^)#\s]*)(?:#([^)\s]*))?\)`)

// The shipped articles must load, link only to articles and sections that exist, and have every screenshot.
func TestEmbeddedContent(t *testing.T) {
	lib, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	if len(lib.Articles()) < 10 {
		t.Fatalf("only %d articles loaded", len(lib.Articles()))
	}
	for _, a := range lib.Articles() {
		if a.Summary == "" {
			t.Errorf("%s: no summary", a.Slug)
		}
		if len(a.Permissions) == 0 && !publicArticles[a.Slug] {
			t.Errorf("%s: has no permissions, so every signed-in person can read it; set permissions or add it to publicArticles", a.Slug)
		}
		for _, s := range a.Sections {
			// Links in any other form (reference style, titles, autolinks) would escape the hidden-article rewrite
			if n, strict := strings.Count(s.Body, "/help/"), len(helpLinkPattern.FindAllString(s.Body, -1)); n != strict {
				t.Errorf("%s#%s: %d mentions of /help/ but only %d [text](/help/slug#section) links", a.Slug, s.ID, n, strict)
			}
			for _, m := range anyHelpLink.FindAllStringSubmatch(s.Body, -1) {
				target, ok := lib.article(m[1])
				if !ok {
					t.Errorf("%s#%s links to missing article %q", a.Slug, s.ID, m[1])
					continue
				}
				if m[2] != "" && !slices.ContainsFunc(target.Sections, func(ts Section) bool { return ts.ID == m[2] }) {
					t.Errorf("%s#%s links to missing section %s#%s", a.Slug, s.ID, m[1], m[2])
				}
			}
		}
	}
	for _, name := range lib.ReferencedImages() {
		if _, err := lib.Image(name); err != nil {
			t.Errorf("screenshot %s is referenced but missing (run the e2e help-screenshots spec)", name)
		}
	}
}
