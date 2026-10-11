package delivery

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Reports are written in Markdown. Email sends it as plain text (Markdown reads well as text); Teams Adaptive Card
// text blocks understand a subset (bold, italic, lists, links) but not headings; Slack uses its own mrkdwn.

var (
	headingLine = regexp.MustCompile(`^#{1,6}\s+(.*?)\s*#*\s*$`)
	boldStars   = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	boldUnders  = regexp.MustCompile(`__([^_\n]+)__`)
	mdLink      = regexp.MustCompile(`\[([^\]\n]+)\]\((https?://[^\s)]+)\)`)
	bulletLine  = regexp.MustCompile(`^(\s*)[-*+]\s+`)
)

// toTeams makes Markdown fit an Adaptive Card text block: headings become bold lines.
func toTeams(md string) string {
	lines := strings.Split(md, "\n")
	for i, line := range lines {
		if m := headingLine.FindStringSubmatch(line); m != nil && strings.TrimSpace(m[1]) != "" {
			lines[i] = "**" + strings.Trim(m[1], "*") + "**"
		}
	}
	return strings.Join(lines, "\n")
}

// toSlack turns Markdown into Slack mrkdwn: &, < and > escaped, headings and **bold** as *bold*, [text](url) as
// <url|text>, and bullets as •. Code blocks are left as they are (only escaped).
func toSlack(md string) string {
	lines := strings.Split(md, "\n")
	inCode := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inCode = !inCode
			continue
		}
		line = escapeSlack(line)
		if inCode {
			lines[i] = line
			continue
		}
		if m := headingLine.FindStringSubmatch(line); m != nil && strings.TrimSpace(m[1]) != "" {
			line = "*" + strings.Trim(m[1], "*") + "*"
		}
		line = boldStars.ReplaceAllString(line, "*$1*")
		line = boldUnders.ReplaceAllString(line, "*$1*")
		line = mdLink.ReplaceAllStringFunc(line, slackLink)
		line = bulletLine.ReplaceAllString(line, "$1• ")
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

func escapeSlack(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// slackLink rewrites an (already escaped) Markdown link; a URL holding | or > is left as text.
func slackLink(m string) string {
	parts := mdLink.FindStringSubmatch(m)
	text, href := parts[1], strings.ReplaceAll(parts[2], "&amp;", "&")
	if strings.ContainsAny(href, "|<>") {
		return m
	}
	return "<" + href + "|" + strings.ReplaceAll(text, "|", "/") + ">"
}

// clip cuts text to at most maxBytes bytes on a line or rune boundary and adds note when it cut.
func clip(text string, maxBytes int, note string) string {
	if len(text) <= maxBytes {
		return text
	}
	limit := maxBytes - len(note) - 2
	if limit < 0 {
		limit = 0
	}
	cut := text[:limit]
	for !utf8.ValidString(cut) && len(cut) > 0 {
		cut = cut[:len(cut)-1]
	}
	if i := strings.LastIndex(cut, "\n"); i > limit/2 {
		cut = cut[:i]
	}
	return strings.TrimRightFunc(cut, unicode.IsSpace) + "\n\n" + note
}

// oneLine makes a title safe for a mail subject or a heading: control characters become spaces.
func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)), " ")
}
