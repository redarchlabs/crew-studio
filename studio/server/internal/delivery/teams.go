package delivery

import (
	"context"
	"strings"
)

// Microsoft Teams (docs/delivery.md): Office 365 connectors (incoming webhooks) were retired in May 2026. Messages
// go to a Teams workflow made from a "When a Teams webhook request is received" template (Power Automate),
// as a message with one Adaptive Card attachment. The workflow's URL carries its own signature (sig=), so it is
// stored as a secret; the workflow must allow "Anyone" to trigger it, since Studio sends no Entra token.

const (
	adaptiveCardType = "application/vnd.microsoft.card.adaptive"
	// Teams refuses messages over about 28 KB; the card's text stays under this, leaving room for the JSON
	maxTeamsBytes = 20_000
	teamsCutNote  = "_The report was cut to fit Teams; the full report is in Crew Studio._"
)

// retiredTeamsHosts are hosts of URLs that no longer accept messages, with why.
var retiredTeamsHosts = []struct{ suffix, why string }{
	{".webhook.office.com", "this is an Office 365 connector URL; connectors were retired in May 2026. Make a Teams workflow from the \"Send webhook alerts to a channel\" template and use its URL"},
	{"outlook.office.com", "this is an Office 365 connector URL; connectors were retired in May 2026. Make a Teams workflow and use its URL"},
	{".logic.azure.com", "Workflows URLs on logic.azure.com stopped working on 30 November 2025. Copy the new URL from the workflow's trigger"},
}

// teamsCard is the request body: a message with one Adaptive Card (schema 1.2, which Teams' own samples use).
func teamsCard(msg Message) map[string]any {
	body := []map[string]any{
		{"type": "TextBlock", "text": oneLine(msg.Title), "weight": "Bolder", "size": "Medium", "wrap": true},
		{"type": "TextBlock", "text": clip(toTeams(msg.Markdown), maxTeamsBytes, teamsCutNote), "wrap": true},
	}
	if msg.Footer != "" {
		body = append(body, map[string]any{"type": "TextBlock", "text": oneLine(msg.Footer), "isSubtle": true, "size": "Small", "wrap": true})
	}
	return map[string]any{
		"type": "message",
		"attachments": []map[string]any{{
			"contentType": adaptiveCardType,
			"contentUrl":  nil,
			"content": map[string]any{
				"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
				"type":    "AdaptiveCard",
				"version": "1.2",
				"body":    body,
			},
		}},
	}
}

func (s *Sender) sendTeams(ctx context.Context, secrets map[string]string, msg Message) error {
	raw := secrets["post_url"]
	if raw == "" {
		return failf(false, "the Teams connection has no workflow URL; replace its secret")
	}
	u, err := s.webhookURL("Teams", raw)
	if err != nil {
		return err
	}
	host := strings.ToLower(u.Hostname())
	for _, r := range retiredTeamsHosts {
		if strings.HasSuffix(host, r.suffix) || host == strings.TrimPrefix(r.suffix, ".") {
			return failf(false, "%s", r.why)
		}
	}
	if !teamsHost(host) && !s.listedOrigin(u) {
		return failf(false, "a Teams workflow URL must be on a Power Platform host (*.powerplatform.com); ask whoever runs Crew Studio to "+
			"list other hosts in STUDIO_DELIVERY_ORIGINS")
	}
	a, err := s.postJSON(ctx, "Teams", u.String(), teamsCard(msg), nil)
	if err != nil {
		return err
	}
	// The workflow answers 202 Accepted: Teams took the request; the flow posts it to the channel
	if a.status < 200 || a.status > 299 {
		return statusProblem("Teams", a, "")
	}
	return nil
}

// teamsHost reports whether host is where Teams workflow (Power Automate) webhooks live. Pinning it keeps a Teams
// connection from being pointed at an internal service.
func teamsHost(host string) bool {
	return strings.HasSuffix(host, ".powerplatform.com")
}
