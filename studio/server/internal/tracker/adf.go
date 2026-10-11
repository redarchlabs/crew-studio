package tracker

import (
	"strings"
)

// Atlassian Document Format (ADF): Jira's rich text. Comments and descriptions arrive as ADF and are read as plain
// text; comments are posted as ADF built from plain text, with mention nodes for the people to notify.
// https://developer.atlassian.com/cloud/jira/platform/apis/document/structure/

// adfNode is one node of an ADF document (read side: only what plain text needs).
type adfNode struct {
	Type    string         `json:"type"`
	Text    string         `json:"text,omitempty"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Content []adfNode      `json:"content,omitempty"`
}

// blockTypes end with a line break when read as text.
var blockTypes = map[string]bool{"paragraph": true, "heading": true, "listItem": true, "codeBlock": true, "blockquote": true,
	"rule": true, "tableRow": true, "panel": true, "mediaSingle": true}

// adfText reads an ADF document as plain text: text, mentions as their @name, emoji as their shortcut, one line
// per paragraph.
func adfText(n adfNode) string {
	var b strings.Builder
	writeADF(&b, n)
	lines := strings.Split(b.String(), "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if l = strings.TrimRight(l, " \t"); l != "" || (len(out) > 0 && out[len(out)-1] != "") {
			out = append(out, l)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func writeADF(b *strings.Builder, n adfNode) {
	switch n.Type {
	case "text":
		b.WriteString(n.Text)
	case "hardBreak":
		b.WriteString("\n")
	case "mention", "emoji", "status", "date":
		for _, key := range []string{"text", "shortName"} {
			if s, ok := n.Attrs[key].(string); ok && s != "" {
				b.WriteString(s)
				break
			}
		}
	case "inlineCard", "blockCard":
		if s, ok := n.Attrs["url"].(string); ok {
			b.WriteString(s)
		}
	}
	for _, c := range n.Content {
		writeADF(b, c)
	}
	if blockTypes[n.Type] {
		b.WriteString("\n")
	}
}

// adfDoc builds a comment: the mentions first, then the text, one paragraph per blank-line-separated block and a
// hard break per line. Text is only ever text nodes, so nothing in it can become a mention or a link.
func adfDoc(text string, mentions []Person) map[string]any {
	paragraphs := []any{}
	first := []any{}
	for _, p := range mentions {
		name := p.Name
		if name == "" {
			name = "user"
		}
		first = append(first, map[string]any{"type": "mention", "attrs": map[string]any{"id": p.AccountID, "text": "@" + name}},
			map[string]any{"type": "text", "text": " "})
	}
	blocks := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n")
	for i, block := range blocks {
		block = strings.Trim(block, "\n")
		if block == "" && !(i == 0 && len(first) > 0) {
			continue
		}
		content := []any{}
		if i == 0 {
			content = append(content, first...)
		}
		for j, line := range strings.Split(block, "\n") {
			if j > 0 {
				content = append(content, map[string]any{"type": "hardBreak"})
			}
			if line != "" {
				content = append(content, map[string]any{"type": "text", "text": line})
			}
		}
		paragraphs = append(paragraphs, map[string]any{"type": "paragraph", "content": content})
	}
	return map[string]any{"version": 1, "type": "doc", "content": paragraphs}
}
