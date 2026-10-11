// Package help serves the in-app help: Markdown articles with screenshots, a table of contents and search.
// Articles and sections declare the permissions they need, and everything a person is shown (contents,
// articles, sections, screenshots, search results and even links between articles) is filtered by them.
package help

import (
	"bufio"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"crewstudio/internal/auth"
)

//go:embed content
var embedded embed.FS

// ImageURLPrefix is where the API serves screenshots; image references in articles are rewritten to it.
const ImageURLPrefix = "/api/help/images/"

// IntroID is the id of the text before an article's first heading.
const IntroID = "intro"

var frontMatterKeys = []string{"title", "category", "order", "permissions", "summary", "keywords"}

// categoryOrder is the order of the table of contents; unknown categories follow, alphabetically.
var categoryOrder = []string{"Getting started", "Console", "Build", "Automate", "Configure", "Developers", "Admin"}

var (
	// ![Caption](name.png): screenshots are bare file names inside content/images
	imagePattern = regexp.MustCompile(`!\[([^\]]*)\]\(([a-z0-9][a-z0-9-]*\.(?:png|jpg|webp))\)`)
	// [text](/help/slug) or [text](/help/slug#section)
	helpLinkPattern  = regexp.MustCompile(`\[([^\]]+)\]\(/help/([a-z0-9-]+)(#[a-z0-9-]+)?\)`)
	sectionPermsLine = regexp.MustCompile(`^<!--\s*permissions:\s*(.*?)\s*-->$`)
	slugPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	nonAlnum         = regexp.MustCompile(`[^a-z0-9]+`)
)

// Viewer is whoever is reading help; *auth.Principal satisfies it.
type Viewer interface {
	Can(permission string) bool
}

// Section is one "## " part of an article (or the intro before the first heading).
type Section struct {
	ID          string
	Title       string
	Body        string
	Permissions []string
	Images      []string
}

// Article is one help page.
type Article struct {
	Slug        string
	Title       string
	Category    string
	Order       int
	Summary     string
	Keywords    []string
	Permissions []string
	Sections    []Section
}

// Library holds every article and the screenshots they use.
type Library struct {
	articles []Article
	bySlug   map[string]int
	images   fs.FS
}

// Heading is a section entry in the table of contents.
type Heading struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// ContentsArticle is an article entry in the table of contents.
type ContentsArticle struct {
	Slug     string    `json:"slug"`
	Title    string    `json:"title"`
	Summary  string    `json:"summary"`
	Headings []Heading `json:"headings"`
}

// ContentsCategory groups articles in the table of contents.
type ContentsCategory struct {
	Title    string            `json:"title"`
	Articles []ContentsArticle `json:"articles"`
}

// SectionView is a section as one person sees it.
type SectionView struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Markdown string `json:"markdown"`
}

// ArticleView is an article as one person sees it.
type ArticleView struct {
	Slug     string        `json:"slug"`
	Title    string        `json:"title"`
	Category string        `json:"category"`
	Summary  string        `json:"summary"`
	Sections []SectionView `json:"sections"`
}

// LoadEmbedded loads the articles compiled into the binary.
func LoadEmbedded() (*Library, error) {
	content, err := fs.Sub(embedded, "content")
	if err != nil {
		return nil, err
	}
	return Load(content)
}

// Load reads every *.md file at the root of fsys; screenshots live in fsys/images.
func Load(fsys fs.FS) (*Library, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read help content: %w", err)
	}
	lib := &Library{bySlug: map[string]int{}}
	if images, err := fs.Sub(fsys, "images"); err == nil {
		lib.images = images
	}
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".md" {
			continue
		}
		raw, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			return nil, err
		}
		a, err := parseArticle(strings.TrimSuffix(e.Name(), ".md"), string(raw))
		if err != nil {
			return nil, fmt.Errorf("help article %s: %w", e.Name(), err)
		}
		lib.articles = append(lib.articles, a)
	}
	slices.SortStableFunc(lib.articles, compareArticles)
	for i, a := range lib.articles {
		lib.bySlug[a.Slug] = i
	}
	return lib, nil
}

func compareArticles(a, b Article) int {
	if c := categoryRank(a.Category) - categoryRank(b.Category); c != 0 {
		return c
	}
	if a.Category != b.Category {
		return strings.Compare(a.Category, b.Category)
	}
	if a.Order != b.Order {
		return a.Order - b.Order
	}
	return strings.Compare(a.Title, b.Title)
}

func categoryRank(c string) int {
	if i := slices.Index(categoryOrder, c); i >= 0 {
		return i
	}
	return len(categoryOrder)
}

func parseArticle(slug, raw string) (Article, error) {
	if !slugPattern.MatchString(slug) {
		return Article{}, fmt.Errorf("file name must be lowercase letters, digits and hyphens")
	}
	meta, body, err := splitFrontMatter(raw)
	if err != nil {
		return Article{}, err
	}
	// A misspelt key such as "permision" would otherwise leave an article open to everyone
	for key := range meta {
		if !slices.Contains(frontMatterKeys, key) {
			return Article{}, fmt.Errorf("unknown front matter key %q (allowed: %s)", key, strings.Join(frontMatterKeys, ", "))
		}
	}
	if v, ok := meta["permissions"]; ok && len(splitList(v)) == 0 {
		return Article{}, fmt.Errorf("permissions is empty; list at least one, or leave the line out for an article everyone may read")
	}
	a := Article{Slug: slug, Title: meta["title"], Category: meta["category"], Summary: meta["summary"]}
	if a.Title == "" || a.Category == "" {
		return Article{}, fmt.Errorf("front matter needs title and category")
	}
	if v := meta["order"]; v != "" {
		if a.Order, err = strconv.Atoi(v); err != nil {
			return Article{}, fmt.Errorf("order: %w", err)
		}
	}
	a.Keywords = splitList(meta["keywords"])
	if a.Permissions, err = parsePermissions(meta["permissions"]); err != nil {
		return Article{}, err
	}
	if a.Sections, err = splitSections(body); err != nil {
		return Article{}, err
	}
	return a, nil
}

func splitFrontMatter(raw string) (map[string]string, string, error) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.HasPrefix(raw, "---\n") {
		return nil, "", fmt.Errorf("missing front matter")
	}
	head, body, found := strings.Cut(raw[4:], "\n---\n")
	if !found {
		return nil, "", fmt.Errorf("front matter is not closed with ---")
	}
	meta := map[string]string{}
	for _, line := range strings.Split(head, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, "", fmt.Errorf("front matter line %q is not key: value", line)
		}
		meta[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return meta, body, nil
}

func splitList(v string) []string {
	var out []string
	for _, item := range strings.Split(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func parsePermissions(v string) ([]string, error) {
	perms := splitList(v)
	for _, p := range perms {
		if !auth.Known(p) {
			return nil, fmt.Errorf("unknown permission %q", p)
		}
	}
	return perms, nil
}

// splitSections cuts the body at "## " headings outside fenced code blocks.
func splitSections(body string) ([]Section, error) {
	sections := []Section{{ID: IntroID}}
	var lines []string
	fence := "" // the open code fence's marker (``` or ~~~, possibly longer), or "" outside one
	expectPerms := false
	used := map[string]int{IntroID: 1}
	flush := func() {
		cur := &sections[len(sections)-1]
		cur.Body = strings.TrimSpace(strings.Join(lines, "\n"))
		lines = nil
	}
	scanner := bufio.NewScanner(strings.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		fence = trackFence(fence, trimmed)
		inFence := fence != ""
		if !inFence && (strings.HasPrefix(line, "## ") || trimmed == "##") {
			flush()
			title := strings.TrimSpace(strings.TrimPrefix(trimmed, "##"))
			if title == "" {
				return nil, fmt.Errorf("a ## heading has no title")
			}
			sections = append(sections, Section{ID: uniqueID(Slugify(title), used), Title: title})
			expectPerms = true
			continue
		}
		if expectPerms && trimmed != "" {
			expectPerms = false
			if m := sectionPermsLine.FindStringSubmatch(trimmed); m != nil {
				perms, err := parsePermissions(m[1])
				if err == nil && len(perms) == 0 {
					err = fmt.Errorf("the permissions comment lists none")
				}
				if err != nil {
					return nil, fmt.Errorf("section %q: %w", sections[len(sections)-1].Title, err)
				}
				sections[len(sections)-1].Permissions = perms
				continue
			}
		}
		if !inFence && strings.HasPrefix(trimmed, "<!--") {
			return nil, fmt.Errorf("a permissions comment must be the first line after a ## heading (near %q)", trimmed)
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	flush()
	out := sections[:0]
	for _, s := range sections {
		if s.ID == IntroID && s.Body == "" {
			continue
		}
		for _, m := range imagePattern.FindAllStringSubmatch(s.Body, -1) {
			if !slices.Contains(s.Images, m[2]) {
				s.Images = append(s.Images, m[2])
			}
		}
		out = append(out, s)
	}
	return out, nil
}

func uniqueID(id string, used map[string]int) string {
	if id == "" {
		id = "section"
	}
	candidate := id
	for n := 2; used[candidate] > 0; n++ {
		candidate = fmt.Sprintf("%s-%d", id, n)
	}
	used[candidate]++
	return candidate
}

// trackFence returns the open fence marker after this line: a line of 3+ backticks or tildes opens a fence,
// and only a line with the same character, at least as long, closes it.
func trackFence(open, line string) string {
	marker := fenceMarker(line)
	switch {
	case marker == "":
		return open
	case open == "":
		return marker
	case marker[0] == open[0] && len(marker) >= len(open) && strings.TrimSpace(line) == marker:
		return ""
	}
	return open
}

func fenceMarker(line string) string {
	for _, c := range []byte{'`', '~'} {
		n := 0
		for n < len(line) && line[n] == c {
			n++
		}
		if n >= 3 {
			return line[:n]
		}
	}
	return ""
}

// Slugify turns a heading into an anchor id: lowercase, non-alphanumerics collapsed to single hyphens.
func Slugify(s string) string {
	return strings.Trim(nonAlnum.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

func holdsAny(v Viewer, perms []string) bool {
	if len(perms) == 0 {
		return v != nil
	}
	for _, p := range perms {
		if v != nil && v.Can(p) {
			return true
		}
	}
	return false
}

func (a *Article) visibleTo(v Viewer) bool { return holdsAny(v, a.Permissions) }

func (a *Article) sectionVisible(v Viewer, s *Section) bool {
	return a.visibleTo(v) && holdsAny(v, s.Permissions)
}

func (a *Article) anchorVisible(v Viewer, id string) bool {
	for i := range a.Sections {
		if a.Sections[i].ID == id {
			return a.sectionVisible(v, &a.Sections[i])
		}
	}
	return false
}

// Articles returns every article (for tests and indexing); callers must filter by viewer themselves.
func (l *Library) Articles() []Article { return l.articles }

func (l *Library) article(slug string) (*Article, bool) {
	i, ok := l.bySlug[slug]
	if !ok {
		return nil, false
	}
	return &l.articles[i], true
}

// Contents is the table of contents the viewer may see.
func (l *Library) Contents(v Viewer) []ContentsCategory {
	out := []ContentsCategory{}
	for i := range l.articles {
		a := &l.articles[i]
		if !a.visibleTo(v) {
			continue
		}
		entry := ContentsArticle{Slug: a.Slug, Title: a.Title, Summary: a.Summary, Headings: []Heading{}}
		for j := range a.Sections {
			s := &a.Sections[j]
			if s.Title != "" && a.sectionVisible(v, s) {
				entry.Headings = append(entry.Headings, Heading{ID: s.ID, Title: s.Title})
			}
		}
		if n := len(out); n > 0 && out[n-1].Title == a.Category {
			out[n-1].Articles = append(out[n-1].Articles, entry)
		} else {
			out = append(out, ContentsCategory{Title: a.Category, Articles: []ContentsArticle{entry}})
		}
	}
	return out
}

// Article returns the article as the viewer sees it, or false when it does not exist or is hidden from them.
func (l *Library) Article(v Viewer, slug string) (ArticleView, bool) {
	a, ok := l.article(slug)
	if !ok || !a.visibleTo(v) {
		return ArticleView{}, false
	}
	view := ArticleView{Slug: a.Slug, Title: a.Title, Category: a.Category, Summary: a.Summary, Sections: []SectionView{}}
	for i := range a.Sections {
		s := &a.Sections[i]
		if a.sectionVisible(v, s) {
			view.Sections = append(view.Sections, SectionView{ID: s.ID, Title: s.Title, Markdown: l.render(v, s.Body)})
		}
	}
	return view, true
}

// render points screenshots at the image endpoint and turns links to articles the viewer cannot open into
// plain text, so hidden articles are neither linked nor dead ends.
func (l *Library) render(v Viewer, body string) string {
	body = imagePattern.ReplaceAllString(body, "![$1]("+ImageURLPrefix+"$2)")
	return helpLinkPattern.ReplaceAllStringFunc(body, func(link string) string {
		m := helpLinkPattern.FindStringSubmatch(link)
		a, ok := l.article(m[2])
		if !ok || !a.visibleTo(v) {
			return m[1]
		}
		// A link into a section the viewer cannot see goes to the top of the article instead
		if anchor := strings.TrimPrefix(m[3], "#"); anchor != "" && !a.anchorVisible(v, anchor) {
			return "[" + m[1] + "](/help/" + m[2] + ")"
		}
		return link
	})
}

// ImageVisible reports whether the screenshot appears in a section the viewer may see.
func (l *Library) ImageVisible(v Viewer, name string) bool {
	for i := range l.articles {
		a := &l.articles[i]
		for j := range a.Sections {
			s := &a.Sections[j]
			if slices.Contains(s.Images, name) && a.sectionVisible(v, s) {
				return true
			}
		}
	}
	return false
}

// Image returns a screenshot's bytes.
func (l *Library) Image(name string) ([]byte, error) {
	if l.images == nil || !imagePattern.MatchString("![]("+name+")") {
		return nil, fs.ErrNotExist
	}
	return fs.ReadFile(l.images, name)
}

// ReferencedImages lists every screenshot name the articles use.
func (l *Library) ReferencedImages() []string {
	var out []string
	for _, a := range l.articles {
		for _, s := range a.Sections {
			for _, img := range s.Images {
				if !slices.Contains(out, img) {
					out = append(out, img)
				}
			}
		}
	}
	slices.Sort(out)
	return out
}
