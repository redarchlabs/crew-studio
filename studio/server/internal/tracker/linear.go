package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"crewstudio/internal/model"
)

// Linear's GraphQL API (https://linear.app/developers/graphql). A personal API key goes in the Authorization header
// as it is (no "Bearer"). Lists are Relay connections paged with first/after; a team key (such as ENG) plays the
// part of a Jira project key.

const (
	linearPageSize     = 50
	linearCommentsPage = 50
	// linearInlineComments is how many new comments are read with each issue in a search; when an issue has that many,
	// its comments are read again on their own (CommentsRead stays false), so none are skipped
	linearInlineComments = 20
	maxLinearPages       = 200
)

const linearIssueFields = `id identifier title url dueDate priorityLabel updatedAt
	state { name type } assignee { id name displayName email url } parent { identifier }
	team { key } project { lead { id name displayName email url } } labels(first: 20) { nodes { name } }`

const linearCommentFields = `id body createdAt user { id name displayName email url }`

type linear struct {
	name     string
	endpoint string
	key      string
	h        *httpClient
}

func newLinear(conn model.Connection, secrets map[string]string, opts Options, h *httpClient) (*linear, error) {
	key := secrets["api_key"]
	if key == "" {
		return nil, errorf("%s has no API key saved; replace its secret", conn.Name)
	}
	endpoint := opts.LinearURL
	if endpoint == "" {
		endpoint = DefaultLinearURL
	}
	return &linear{name: conn.Name, endpoint: endpoint, key: key, h: h}, nil
}

func (l *linear) Kind() string { return model.TrackerLinear }

type gqlError struct {
	Message    string `json:"message"`
	Extensions struct {
		Code string `json:"code"`
		Type string `json:"type"`
	} `json:"extensions"`
}

// linearRateLimited reports Linear's "slow down" answer: HTTP 400 with extensions.code RATELIMITED.
func linearRateLimited(r response) bool {
	if r.status != http.StatusBadRequest {
		return false
	}
	var body struct {
		Errors []gqlError `json:"errors"`
	}
	_ = json.Unmarshal(r.body, &body)
	for _, e := range body.Errors {
		if strings.EqualFold(e.Extensions.Code, "RATELIMITED") || strings.EqualFold(e.Extensions.Type, "ratelimited") {
			return true
		}
	}
	return false
}

// query runs one GraphQL operation and decodes its data into out.
func (l *linear) query(ctx context.Context, q string, vars map[string]any, out any) error {
	payload, err := json.Marshal(map[string]any{"query": q, "variables": vars})
	if err != nil {
		return fmt.Errorf("encode linear request: %w", err)
	}
	resp, err := l.h.do(ctx, l.name, request{method: http.MethodPost, url: l.endpoint, body: payload,
		headers: map[string]string{"Authorization": l.key}, rateLimited: linearRateLimited})
	if err != nil {
		return err
	}
	var body struct {
		Data   json.RawMessage `json:"data"`
		Errors []gqlError      `json:"errors"`
	}
	decodeErr := json.Unmarshal(resp.body, &body)
	if len(body.Errors) > 0 || resp.status < 200 || resp.status >= 300 {
		return linearError(l.name, resp.status, body.Errors)
	}
	if decodeErr != nil || len(body.Data) == 0 || string(body.Data) == "null" {
		return errorf("%s answered with something that is not the JSON Linear sends", l.name)
	}
	if err := json.Unmarshal(body.Data, out); err != nil {
		return errorf("%s answered with data Crew Studio did not expect", l.name)
	}
	return nil
}

func linearError(name string, status int, errs []gqlError) error {
	// GraphQL errors (answered with HTTP 200) mean the mutation was not carried out, as a 4xx does
	e := rejectedIf(&Error{Status: status}, status)
	e.Rejected = e.Rejected || len(errs) > 0
	code, msg := "", ""
	if len(errs) > 0 {
		code = strings.ToUpper(errs[0].Extensions.Code)
		msg = oneLine(errs[0].Message, 300)
		if code == "" {
			code = strings.ToUpper(strings.ReplaceAll(errs[0].Extensions.Type, " ", "_"))
		}
	}
	switch {
	case status == http.StatusUnauthorized || code == "AUTHENTICATION_ERROR":
		e.Auth = true
		e.Message = name + " refused the API key; replace it"
	case status == http.StatusForbidden || code == "FORBIDDEN":
		e.Auth = true
		e.Message = name + " does not allow this with its API key"
	case strings.Contains(strings.ToLower(msg), "not found") || code == "ENTITY_NOT_FOUND":
		e.NotFound = true
		e.Message = name + ": not found"
	case msg != "":
		e.Message = fmt.Sprintf("%s answered: %s", name, msg)
	default:
		e.Message = fmt.Sprintf("%s answered HTTP %d", name, status)
	}
	return e
}

type linearUser struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	URL         string `json:"url"`
	Active      *bool  `json:"active"`
}

func (u *linearUser) person() *Person {
	if u == nil || u.ID == "" {
		return nil
	}
	name := u.Name
	if name == "" {
		name = u.DisplayName
	}
	return &Person{AccountID: u.ID, Name: name, Email: u.Email, URL: u.URL}
}

type linearComment struct {
	ID        string      `json:"id"`
	Body      string      `json:"body"`
	CreatedAt time.Time   `json:"createdAt"`
	User      *linearUser `json:"user"`
}

func (c linearComment) comment() Comment {
	out := Comment{ID: c.ID, Body: c.Body, At: c.CreatedAt}
	if p := c.User.person(); p != nil {
		out.Author = *p
	}
	return out
}

type linearIssue struct {
	ID            string                       `json:"id"`
	Identifier    string                       `json:"identifier"`
	Title         string                       `json:"title"`
	URL           string                       `json:"url"`
	DueDate       *string                      `json:"dueDate"`
	PriorityLabel string                       `json:"priorityLabel"`
	UpdatedAt     time.Time                    `json:"updatedAt"`
	Description   *string                      `json:"description"`
	State         *struct{ Name, Type string } `json:"state"`
	Assignee      *linearUser                  `json:"assignee"`
	Parent        *struct{ Identifier string } `json:"parent"`
	Team          *struct{ Key string }        `json:"team"`
	Project       *struct {
		Lead *linearUser `json:"lead"`
	} `json:"project"`
	Labels *struct {
		Nodes []struct{ Name string } `json:"nodes"`
	} `json:"labels"`
	Comments *struct {
		Nodes []linearComment `json:"nodes"`
	} `json:"comments"`
}

func linearCategory(stateType string) string {
	switch stateType {
	case "started":
		return model.StatusInProgress
	case "completed":
		return model.StatusDone
	case "canceled", "duplicate":
		return model.StatusCanceled
	}
	return model.StatusToDo
}

func (raw linearIssue) issue() Issue {
	out := Issue{ID: raw.ID, Key: raw.Identifier, Title: raw.Title, URL: raw.URL, Priority: raw.PriorityLabel,
		UpdatedAt: raw.UpdatedAt, Assignee: raw.Assignee.person(), Labels: []string{}, StatusCategory: model.StatusToDo}
	if raw.DueDate != nil {
		out.DueDate = *raw.DueDate
	}
	if raw.State != nil {
		out.Status, out.StatusCategory = raw.State.Name, linearCategory(raw.State.Type)
	}
	if raw.Parent != nil {
		out.Parent = raw.Parent.Identifier
	}
	if raw.Team != nil {
		out.Project = raw.Team.Key
	}
	if raw.Project != nil {
		out.Lead = raw.Project.Lead.person()
	}
	if raw.Labels != nil {
		for _, n := range raw.Labels.Nodes {
			out.Labels = append(out.Labels, n.Name)
		}
	}
	if raw.Description != nil {
		out.Description = oneLineLimit(*raw.Description, maxDescription)
	}
	if raw.Comments != nil && len(raw.Comments.Nodes) < linearInlineComments {
		out.CommentsRead = true
		for _, c := range raw.Comments.Nodes {
			out.Comments = append(out.Comments, c.comment())
		}
		out.Comments = newestFirst(out.Comments, len(out.Comments))
	}
	return out
}

type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

func (l *linear) Myself(ctx context.Context) (Person, error) {
	var res struct {
		Viewer linearUser `json:"viewer"`
	}
	if err := l.query(ctx, `query { viewer { id name displayName email url } }`, nil, &res); err != nil {
		return Person{}, err
	}
	if p := res.Viewer.person(); p != nil {
		return *p, nil
	}
	return Person{}, errorf("%s did not say which account the key belongs to", l.name)
}

// Projects lists teams: their keys start every issue identifier, as Jira project keys do.
func (l *linear) Projects(ctx context.Context, max int) ([]Project, error) {
	out := []Project{}
	after := ""
	for page := 0; page < maxLinearPages && len(out) < max; page++ {
		var res struct {
			Teams struct {
				Nodes    []struct{ ID, Key, Name string } `json:"nodes"`
				PageInfo pageInfo                         `json:"pageInfo"`
			} `json:"teams"`
		}
		q := `query($after: String) { teams(first: 50, after: $after) { nodes { id key name } pageInfo { hasNextPage endCursor } } }`
		if err := l.query(ctx, q, map[string]any{"after": nullable(after)}, &res); err != nil {
			return nil, err
		}
		for _, t := range res.Teams.Nodes {
			out = append(out, Project{ID: t.ID, Key: t.Key, Name: t.Name})
		}
		if !res.Teams.PageInfo.HasNextPage {
			break
		}
		after = res.Teams.PageInfo.EndCursor
	}
	return out[:min(len(out), max)], nil
}

func (l *linear) Project(ctx context.Context, key string) (Project, error) {
	var res struct {
		Teams struct {
			Nodes []struct{ ID, Key, Name string } `json:"nodes"`
		} `json:"teams"`
	}
	q := `query($key: String!) { teams(filter: { key: { eq: $key } }, first: 1) { nodes { id key name } } }`
	if err := l.query(ctx, q, map[string]any{"key": key}, &res); err != nil {
		return Project{}, err
	}
	if len(res.Teams.Nodes) == 0 {
		return Project{}, &Error{Message: l.name + ": no team with the key " + key, NotFound: true}
	}
	t := res.Teams.Nodes[0]
	return Project{ID: t.ID, Key: t.Key, Name: t.Name}, nil
}

func (l *linear) Issue(ctx context.Context, ref string) (Issue, error) {
	var res struct {
		Issue *linearIssue `json:"issue"`
	}
	q := `query($id: String!) { issue(id: $id) { ` + linearIssueFields + ` description } }`
	if err := l.query(ctx, q, map[string]any{"id": ref}, &res); err != nil {
		return Issue{}, err
	}
	if res.Issue == nil {
		return Issue{}, &Error{Message: l.name + ": not found", NotFound: true}
	}
	return res.Issue.issue(), nil
}

// linearFilter is the IssueFilter of a query.
func linearFilter(q Query) map[string]any {
	filter := map[string]any{}
	if q.Parent != nil {
		filter["parent"] = map[string]any{"id": map[string]any{"eq": q.Parent.ID}}
	} else {
		filter["team"] = map[string]any{"key": map[string]any{"eq": q.Project}}
	}
	if !q.IncludeDone {
		filter["state"] = map[string]any{"type": map[string]any{"nin": []string{"completed", "canceled", "duplicate"}}}
	}
	return filter
}

// Search pages through issues(filter:). With CommentsSince, each issue also brings the comments created after it.
func (l *linear) Search(ctx context.Context, q Query) ([]Issue, error) {
	if strings.TrimSpace(q.JQL) != "" {
		return nil, errorf("JQL only applies to Jira connections; %s is a Linear connection", l.name)
	}
	fields := linearIssueFields
	vars := map[string]any{"filter": linearFilter(q)}
	decl := `$filter: IssueFilter, $after: String`
	if q.CommentsSince != nil {
		decl += `, $since: DateTimeOrDuration`
		vars["since"] = q.CommentsSince.UTC().Format(time.RFC3339Nano)
		fields += fmt.Sprintf(` comments(first: %d, filter: { createdAt: { gt: $since } }) { nodes { %s } }`, linearInlineComments, linearCommentFields)
	}
	query := `query(` + decl + `) { issues(filter: $filter, first: ` + fmt.Sprint(linearPageSize) +
		`, after: $after) { nodes { ` + fields + ` } pageInfo { hasNextPage endCursor } } }`
	out := []Issue{}
	after := ""
	for page := 0; page < maxLinearPages; page++ {
		vars["after"] = nullable(after)
		var res struct {
			Issues struct {
				Nodes    []linearIssue `json:"nodes"`
				PageInfo pageInfo      `json:"pageInfo"`
			} `json:"issues"`
		}
		if err := l.query(ctx, query, vars, &res); err != nil {
			return nil, err
		}
		for _, raw := range res.Issues.Nodes {
			out = append(out, raw.issue())
		}
		if q.Max > 0 && len(out) > q.Max {
			return nil, &LimitError{Count: len(out), Max: q.Max}
		}
		if !res.Issues.PageInfo.HasNextPage {
			return out, nil
		}
		after = res.Issues.PageInfo.EndCursor
	}
	return nil, errorf("%s returned too many pages of issues", l.name)
}

func (l *linear) Comments(ctx context.Context, ref string, limit int) ([]Comment, error) {
	var res struct {
		Issue *struct {
			Comments struct {
				Nodes []linearComment `json:"nodes"`
			} `json:"comments"`
		} `json:"issue"`
	}
	q := fmt.Sprintf(`query($id: String!) { issue(id: $id) { comments(first: %d, orderBy: createdAt) { nodes { %s } } } }`,
		linearCommentsPage, linearCommentFields)
	if err := l.query(ctx, q, map[string]any{"id": ref}, &res); err != nil {
		return nil, err
	}
	if res.Issue == nil {
		return nil, &Error{Message: l.name + ": not found", NotFound: true}
	}
	out := make([]Comment, 0, len(res.Issue.Comments.Nodes))
	for _, c := range res.Issue.Comments.Nodes {
		out = append(out, c.comment())
	}
	return newestFirst(out, limit), nil
}

// PostComment posts Markdown. Mentions are the people's profile URLs, which Linear turns into @mentions.
func (l *linear) PostComment(ctx context.Context, ref, text string, mentions []Person) (Posted, error) {
	parts := []string{}
	for _, p := range mentions {
		if p.URL == "" {
			return Posted{}, errorf("%s did not give a profile address for %s, so they cannot be mentioned", l.name, p.Name)
		}
		parts = append(parts, p.URL)
	}
	body := text
	if len(parts) > 0 {
		body = strings.Join(parts, " ") + "\n\n" + text
	}
	var res struct {
		CommentCreate struct {
			Success bool `json:"success"`
			Comment struct {
				ID  string `json:"id"`
				URL string `json:"url"`
			} `json:"comment"`
		} `json:"commentCreate"`
	}
	q := `mutation($input: CommentCreateInput!) { commentCreate(input: $input) { success comment { id url } } }`
	if err := l.query(ctx, q, map[string]any{"input": map[string]any{"issueId": ref, "body": body}}, &res); err != nil {
		return Posted{}, err
	}
	if !res.CommentCreate.Success {
		e := errorf("%s did not accept the comment", l.name)
		e.Rejected = true
		return Posted{}, e
	}
	return Posted{ID: res.CommentCreate.Comment.ID, URL: res.CommentCreate.Comment.URL}, nil
}

func (l *linear) CreateIssue(ctx context.Context, in NewIssue) (Issue, error) {
	team, err := l.Project(ctx, in.Project)
	if err != nil {
		return Issue{}, err
	}
	input := map[string]any{"teamId": team.ID, "title": in.Title}
	if in.Description != "" {
		input["description"] = in.Description
	}
	if in.Parent != "" {
		input["parentId"] = in.Parent
	}
	var res struct {
		IssueCreate struct {
			Success bool         `json:"success"`
			Issue   *linearIssue `json:"issue"`
		} `json:"issueCreate"`
	}
	q := `mutation($input: IssueCreateInput!) { issueCreate(input: $input) { success issue { ` + linearIssueFields + ` } } }`
	if err := l.query(ctx, q, map[string]any{"input": input}, &res); err != nil {
		return Issue{}, err
	}
	if !res.IssueCreate.Success || res.IssueCreate.Issue == nil {
		return Issue{}, errorf("%s did not create the issue", l.name)
	}
	return res.IssueCreate.Issue.issue(), nil
}

func (l *linear) Users(ctx context.Context, max int) ([]Person, error) {
	out := []Person{}
	after := ""
	for page := 0; page < maxLinearPages && len(out) < max; page++ {
		var res struct {
			Users struct {
				Nodes    []linearUser `json:"nodes"`
				PageInfo pageInfo     `json:"pageInfo"`
			} `json:"users"`
		}
		q := `query($after: String) { users(first: 100, after: $after) { nodes { id name displayName email url active } pageInfo { hasNextPage endCursor } } }`
		if err := l.query(ctx, q, map[string]any{"after": nullable(after)}, &res); err != nil {
			return nil, err
		}
		for _, u := range res.Users.Nodes {
			if p := u.person(); p != nil && (u.Active == nil || *u.Active) {
				out = append(out, *p)
			}
		}
		if !res.Users.PageInfo.HasNextPage {
			break
		}
		after = res.Users.PageInfo.EndCursor
	}
	return out[:min(len(out), max)], nil
}

func (l *linear) User(ctx context.Context, accountID string) (Person, error) {
	var res struct {
		User *linearUser `json:"user"`
	}
	if err := l.query(ctx, `query($id: String!) { user(id: $id) { id name displayName email url } }`, map[string]any{"id": accountID}, &res); err != nil {
		return Person{}, err
	}
	if p := res.User.person(); p != nil {
		return *p, nil
	}
	return Person{}, &Error{Message: "no such person", NotFound: true}
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
