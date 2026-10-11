package chat

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"crewstudio/internal/model"
	"crewstudio/internal/store"
	"crewstudio/internal/tracker"
)

// Tracker cards from agent chat (slice 3d): a comment, a suggested change (posted as a comment; no field is ever
// changed) and a new issue. Confirming posts through the connection's account as the person (the text says who), with
// no action log: an agent has none. They cannot be undone from chat.

type trackerCommentProposal struct {
	proposedVia
	Issue string `json:"issue"`
	Text  string `json:"text"`
}

type trackerSuggestionProposal struct {
	proposedVia
	Issue  string `json:"issue"`
	Field  string `json:"field"`
	Value  string `json:"value"`
	Reason string `json:"reason"`
}

type trackerIssueProposal struct {
	proposedVia
	Project     string `json:"project"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Parent      string `json:"parent"`
}

func trackerCommentKind() Kind {
	return Kind{Type: TypeTrackerComment, Normalize: normalizeTrackerComment, Apply: applyTrackerComment, Needs: counterpartNeeds}
}

func trackerSuggestionKind() Kind {
	return Kind{Type: TypeTrackerSuggestion, Normalize: normalizeTrackerSuggestion, Apply: applyTrackerSuggestion, Needs: counterpartNeeds}
}

func trackerIssueKind() Kind {
	return Kind{Type: TypeTrackerIssue, Normalize: normalizeTrackerIssue, Apply: applyTrackerIssue, Needs: counterpartNeeds}
}

// finish turns the problems found into a validation error, or the clean payload into the stored map.
func finish(problems []string, clean any, summary string) (map[string]any, string, error) {
	if len(problems) > 0 {
		slices.Sort(problems)
		return nil, "", &model.ValidationError{Details: problems}
	}
	m, err := asMap(clean)
	return m, summary, err
}

// detailsOf is the problems of a model validation error (nil when err is nil); any other error is returned as is.
func detailsOf(err error) ([]string, error) {
	if err == nil {
		return nil, nil
	}
	if v, ok := err.(*model.ValidationError); ok {
		return v.Details, nil
	}
	return nil, err
}

// issueRef checks an issue key such as PRJ-10.
func issueRef(field, ref string) (string, []string) {
	clean, ok := model.NormalizeItemRef(ref)
	if !ok {
		return clean, []string{field + ": use the issue's key, such as PRJ-10"}
	}
	return clean, nil
}

// withinRoom refuses a text too long to carry the attribution as well.
func withinRoom(field, text string, limit int) []string {
	if n := len([]rune(text)); n > limit-attributionRoom {
		return []string{fmt.Sprintf("%s: must be at most %d characters", field, limit-attributionRoom)}
	}
	return nil
}

func normalizeTrackerComment(payload map[string]any) (map[string]any, string, error) {
	in, err := decodePayload[trackerCommentProposal](payload)
	if err != nil {
		return nil, "", err
	}
	via, problems := in.clean()
	ref, refProblems := issueRef("issue", in.Issue)
	c, err := model.NormalizeTrackerComment(model.TrackerComment{Text: in.Text})
	details, err := detailsOf(err)
	if err != nil {
		return nil, "", err
	}
	problems = slices.Concat(problems, refProblems, details, withinRoom("text", c.Text, model.MaxTrackerCommentChars))
	return finish(problems, trackerCommentProposal{proposedVia: via, Issue: ref, Text: c.Text}, "Comment on "+ref)
}

func normalizeTrackerSuggestion(payload map[string]any) (map[string]any, string, error) {
	in, err := decodePayload[trackerSuggestionProposal](payload)
	if err != nil {
		return nil, "", err
	}
	via, problems := in.clean()
	ref, refProblems := issueRef("issue", in.Issue)
	c, err := model.NormalizeTrackerSuggestion(model.TrackerSuggestion{Field: in.Field, Value: in.Value, Reason: in.Reason})
	details, err := detailsOf(err)
	if err != nil {
		return nil, "", err
	}
	problems = slices.Concat(problems, refProblems, details)
	out := trackerSuggestionProposal{proposedVia: via, Issue: ref, Field: c.Field, Value: c.Value, Reason: c.Reason}
	return finish(problems, out, fmt.Sprintf("Suggest a change on %s: %s to %s", ref, strings.ReplaceAll(c.Field, "_", " "), c.Value))
}

func normalizeTrackerIssue(payload map[string]any) (map[string]any, string, error) {
	in, err := decodePayload[trackerIssueProposal](payload)
	if err != nil {
		return nil, "", err
	}
	via, problems := in.clean()
	c, err := model.NormalizeTrackerNewIssue(model.TrackerNewIssue{Project: in.Project, Title: in.Title, Description: in.Description, Parent: in.Parent})
	details, err := detailsOf(err)
	if err != nil {
		return nil, "", err
	}
	problems = slices.Concat(problems, details, withinRoom("description", c.Description, model.MaxTrackerCommentChars))
	out := trackerIssueProposal{proposedVia: via, Project: c.Project, Title: c.Title, Description: c.Description, Parent: c.Parent}
	return finish(problems, out, fmt.Sprintf("Create an issue in %s: %s", c.Project, c.Title))
}

// linearProfileLink is a Linear profile URL, which Linear turns into a mention: a card may not notify people that way.
var linearProfileLink = tracker.LinearProfileLink

// confirmedTracker is the tracker a confirmed card posts through, checked again as the person confirming.
func confirmedTracker(ctx context.Context, env Env, req Request, typ string, via proposedVia, text string) (model.Connection, tracker.Client, error) {
	if env.Trackers == nil {
		return model.Connection{}, nil, &DeniedError{Message: "Trackers are not set up on this server."}
	}
	conn, err := counterpartConnection(ctx, env, req, typ, via)
	if err != nil {
		return conn, nil, err
	}
	if conn.Kind == model.TrackerLinear && linearProfileLink.MatchString(text) {
		return conn, nil, &model.ValidationError{Details: []string{"text: Linear profile links would mention people; ask the agent again without them"}}
	}
	if !env.Trackers.Allow(conn.ID) {
		return conn, nil, &store.ConflictError{Code: "tracker_busy", Message: "Too many tracker calls through " + conn.Name + " just now; try again in a minute."}
	}
	_, client, err := env.Trackers.ForAgentChat(ctx, conn.ID)
	return conn, client, err
}

// postAsPerson posts text with the attribution on the issue and says where it went.
func postAsPerson(ctx context.Context, env Env, req Request, typ string, via proposedVia, ref, text string) (map[string]any, error) {
	text += attribution(req.Principal, via.ProposedBy)
	conn, client, err := confirmedTracker(ctx, env, req, typ, via, text)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	issue, err := client.Issue(ctx, ref)
	if err != nil {
		return nil, err
	}
	posted, err := client.PostComment(ctx, issue.Key, text, nil)
	if err != nil {
		return written(nil, err, map[string]any{"connection": conn.Name, "issue": issue.Key, "url": issue.URL})
	}
	return map[string]any{"connection": conn.Name, "issue": issue.Key, "comment_id": posted.ID, "url": posted.URL}, nil
}

func applyTrackerComment(ctx context.Context, env Env, req Request) (map[string]any, error) {
	in, err := decodePayload[trackerCommentProposal](req.Payload)
	if err != nil {
		return nil, err
	}
	return postAsPerson(ctx, env, req, TypeTrackerComment, in.proposedVia, in.Issue, in.Text)
}

func applyTrackerSuggestion(ctx context.Context, env Env, req Request) (map[string]any, error) {
	in, err := decodePayload[trackerSuggestionProposal](req.Payload)
	if err != nil {
		return nil, err
	}
	// As a crew's suggestion reads, with the person who confirmed it in place of the crew
	text := fmt.Sprintf("Suggested change from %s (Crew Studio): set %s to %q.\nReason: %s\n\nCrew Studio does not change fields; "+
		"a person decides whether to make this change.", oneLine(req.Principal.Name), strings.ReplaceAll(in.Field, "_", " "), in.Value, in.Reason)
	return postAsPerson(ctx, env, req, TypeTrackerSuggestion, in.proposedVia, in.Issue, text)
}

func applyTrackerIssue(ctx context.Context, env Env, req Request) (map[string]any, error) {
	in, err := decodePayload[trackerIssueProposal](req.Payload)
	if err != nil {
		return nil, err
	}
	description := strings.TrimSpace(in.Description + attribution(req.Principal, in.ProposedBy))
	conn, client, err := confirmedTracker(ctx, env, req, TypeTrackerIssue, in.proposedVia, in.Title+" "+description)
	if err != nil {
		return nil, err
	}
	if !mayCreateIssues(conn) {
		return nil, createOff(conn.Name)
	}
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	issue, err := client.CreateIssue(ctx, tracker.NewIssue{Project: in.Project, Title: in.Title, Description: description, Parent: in.Parent})
	if err != nil {
		return written(nil, err, map[string]any{"connection": conn.Name, "project": in.Project})
	}
	return map[string]any{"connection": conn.Name, "issue": issue.Key, "url": issue.URL}, nil
}
