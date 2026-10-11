package watch

import (
	"bytes"
	"strings"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// skippedTags hold no readable text.
var skippedTags = map[atom.Atom]bool{atom.Script: true, atom.Style: true, atom.Noscript: true, atom.Svg: true,
	atom.Head: true, atom.Template: true, atom.Iframe: true}

// blockTags end a line of text.
var blockTags = map[atom.Atom]bool{atom.P: true, atom.Div: true, atom.Br: true, atom.Li: true, atom.Tr: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true, atom.Section: true,
	atom.Article: true, atom.Header: true, atom.Footer: true, atom.Table: true, atom.Ul: true, atom.Ol: true,
	atom.Td: true, atom.Th: true, atom.Pre: true, atom.Blockquote: true, atom.Main: true, atom.Nav: true}

// CheckSelector reports a problem with a CSS selector, or "".
func CheckSelector(selector string) string {
	if selector == "" {
		return ""
	}
	if _, err := cascadia.Compile(selector); err != nil {
		return "is not a CSS selector this watch understands: " + err.Error()
	}
	return ""
}

// PageText extracts the readable text of an HTML page, or of every element matching selector.
func PageText(page []byte, selector string) (string, error) {
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return "", sourceErrorf("the page could not be read as HTML")
	}
	roots := []*html.Node{doc}
	if selector != "" {
		sel, err := cascadia.Compile(selector)
		if err != nil {
			return "", sourceErrorf("the CSS selector is not valid: %v", err)
		}
		roots = sel.MatchAll(doc)
		if len(roots) == 0 {
			return "", sourceErrorf("the CSS selector %q matched nothing on the page", selector)
		}
	}
	var b strings.Builder
	for _, n := range roots {
		writeText(&b, n)
		b.WriteByte('\n')
	}
	return cleanLines(b.String()), nil
}

func writeText(b *strings.Builder, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		// HTML collapses whitespace; only block elements start new lines
		if words := strings.Fields(n.Data); len(words) > 0 {
			b.WriteByte(' ')
			b.WriteString(strings.Join(words, " "))
			b.WriteByte(' ')
		}
		return
	case html.ElementNode:
		if skippedTags[n.DataAtom] {
			return
		}
	case html.CommentNode, html.DoctypeNode:
		return
	}
	block := n.Type == html.ElementNode && blockTags[n.DataAtom]
	if block {
		b.WriteByte('\n')
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		writeText(b, c)
	}
	if block {
		b.WriteByte('\n')
	}
}

// cleanLines trims every line, collapses runs of spaces and drops blank lines, so layout-only edits do not count.
func cleanLines(text string) string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if line = strings.Join(strings.Fields(line), " "); line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
