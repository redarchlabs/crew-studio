package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"crewstudio/internal/model"
)

// Slack (docs/delivery.md): an incoming webhook (POST {"text"} to https://hooks.slack.com/services/..., which answers
// 200 "ok" or an error code), or a bot token with chat.postMessage (Authorization: Bearer, JSON {channel, text};
// the answer's "ok" says whether it worked, "error" why not).

const (
	slackWebhookHost = "hooks.slack.com"
	// maxSlackChars keeps a message well under Slack's 40,000-character cut-off
	maxSlackChars = 12_000
	slackCutNote  = "_The report was cut to fit Slack; the full report is in Crew Studio._"
)

func slackText(msg Message) string {
	text := "*" + escapeSlack(oneLine(msg.Title)) + "*\n\n" + toSlack(msg.Markdown)
	if msg.Footer != "" {
		text += "\n\n_" + escapeSlack(oneLine(msg.Footer)) + "_"
	}
	return clip(text, maxSlackChars, slackCutNote)
}

func (s *Sender) sendSlack(ctx context.Context, conn model.Connection, secrets map[string]string, msg Message) error {
	text := slackText(msg)
	if hook := secrets["webhook_url"]; hook != "" {
		return s.slackWebhook(ctx, hook, text)
	}
	if token := secrets["bot_token"]; token != "" {
		return s.slackPost(ctx, conn, token, text)
	}
	return failf(false, "%s has no webhook URL or bot token; replace its secret", conn.Name)
}

func (s *Sender) slackWebhook(ctx context.Context, hook, text string) error {
	u, err := s.webhookURL("Slack", hook)
	if err != nil {
		return err
	}
	if u.Scheme == "https" && !strings.EqualFold(u.Hostname(), slackWebhookHost) && !s.listedOrigin(u) {
		return failf(false, "a Slack webhook URL must start with https://%s/", slackWebhookHost)
	}
	a, err := s.postJSON(ctx, "Slack", u.String(), map[string]any{"text": text}, nil)
	if err != nil {
		return err
	}
	if a.status < 200 || a.status > 299 {
		return statusProblem("Slack", a, plainCode(a.body))
	}
	return nil
}

type slackAnswer struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

func (s *Sender) slackPost(ctx context.Context, conn model.Connection, token, text string) error {
	channel := strings.TrimSpace(conn.Config["channel"])
	if channel == "" {
		return failf(false, "%s has a bot token but no channel; set the channel", conn.Name)
	}
	body := map[string]any{"channel": channel, "text": text, "unfurl_links": false, "unfurl_media": false}
	a, err := s.postJSON(ctx, "Slack", s.opts.SlackAPIURL+"/chat.postMessage", body, map[string]string{"Authorization": "Bearer " + token})
	if err != nil {
		return err
	}
	var got slackAnswer
	_ = json.Unmarshal(a.body, &got)
	code := ""
	if errorCodePattern.MatchString(got.Error) {
		code = got.Error
	}
	switch {
	case a.status == http.StatusOK && got.OK:
		return nil
	case a.status != http.StatusOK:
		return statusProblem("Slack", a, code)
	}
	return slackRefusal(channel, code)
}

// slackRefusal explains chat.postMessage's error codes that people can fix.
func slackRefusal(channel, code string) error {
	switch code {
	case "not_in_channel":
		return failf(false, "the Slack bot is not in %s; invite it to the channel", channel)
	case "channel_not_found":
		return failf(false, "Slack has no channel %s that the bot can see", channel)
	case "invalid_auth", "not_authed", "token_revoked", "account_inactive":
		return failf(false, "Slack refused the bot token (%s); replace the secret", code)
	case "missing_scope":
		return failf(false, "the Slack bot token lacks the chat:write scope")
	case "is_archived":
		return failf(false, "the Slack channel %s is archived", channel)
	case "ratelimited", "rate_limited":
		return failf(true, "Slack is limiting how fast messages are sent; retry later")
	case "":
		return failf(true, "Slack did not accept the message")
	}
	return failf(false, "Slack refused the message (%s)", code)
}
