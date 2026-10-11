package tracker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"crewstudio/internal/model"
)

// Jira Cloud REST v3 (https://developer.atlassian.com/cloud/jira/platform/rest/v3/). Basic auth with the account
// email and an API token; classic tokens call the site, scoped tokens call api.atlassian.com/ex/jira/{cloudId}.

const (
	jiraPageSize    = 100
	jiraIssueFields = "summary,status,assignee,duedate,priority,updated,parent,labels,project"
	jiraTimeLayout  = "2006-01-02T15:04:05.000-0700"
	scopedAPIBase   = "https://api.atlassian.com/ex/jira/"
	maxDescription  = 5_000
	maxJiraPages    = 200
	// maxCommentsPage is the most comments one call reads (Jira's own maximum page)
	maxCommentsPage = 100
)

type jira struct {
	name     string
	site     string
	base     string
	auth     string
	h        *httpClient
	issueTyp string
}

func newJira(conn model.Connection, secrets map[string]string, opts Options, h *httpClient) (*jira, error) {
	site := strings.TrimRight(conn.Config["site_url"], "/")
	if err := CheckJiraSite(site, opts.JiraSites); err != nil {
		return nil, err
	}
	token := secrets["api_token"]
	if token == "" || conn.Config["email"] == "" {
		return nil, errorf("%s has no account email or API token saved; replace its secret", conn.Name)
	}
	base := site
	if id := conn.Config[model.ConfigCloudID]; id != "" {
		base = scopedAPIBase + url.PathEscape(id)
	}
	issueType := conn.Config[model.ConfigIssueType]
	if issueType == "" {
		issueType = "Task"
	}
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(conn.Config["email"]+":"+token))
	return &jira{name: conn.Name, site: site, base: base, auth: auth, h: h, issueTyp: issueType}, nil
}

// CheckJiraSite allows https://<name>.atlassian.net (no port, path or credentials) or an origin an admin listed in
// STUDIO_JIRA_SITES, so a connection cannot send its API token anywhere else.
func CheckJiraSite(site string, allowed []string) error {
	for _, origin := range allowed {
		if strings.EqualFold(strings.TrimRight(origin, "/"), site) {
			return nil
		}
	}
	u, err := url.Parse(site)
	bad := err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || (u.Path != "" && u.Path != "/") ||
		u.RawQuery != "" || u.Fragment != ""
	host := ""
	if !bad {
		host = strings.ToLower(u.Hostname())
	}
	label, isAtlassian := strings.CutSuffix(host, ".atlassian.net")
	if bad || !isAtlassian || label == "" || strings.Contains(label, ".") {
		return errorf("the Jira site must be https://<your-site>.atlassian.net (or a site an administrator allowed in STUDIO_JIRA_SITES)")
	}
	return nil
}

func (j *jira) Kind() string { return model.TrackerJira }

// call sends one request and decodes a 2xx answer into out (when out is not nil).
func (j *jira) call(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	target := j.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return fmt.Errorf("encode jira request: %w", err)
		}
	}
	resp, err := j.h.do(ctx, j.name, request{method: method, url: target, body: payload, headers: map[string]string{"Authorization": j.auth}})
	if err != nil {
		return err
	}
	if resp.status < 200 || resp.status >= 300 {
		return jiraError(j.name, resp)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(resp.body, out); err != nil {
		return errorf("%s answered with something that is not the JSON Jira sends", j.name)
	}
	return nil
}

// jiraError turns an error answer into a safe message: the status plus Jira's own short error messages.
func jiraError(name string, resp response) error {
	var body struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
	}
	_ = json.Unmarshal(resp.body, &body)
	msgs := body.ErrorMessages
	for k, v := range body.Errors {
		msgs = append(msgs, k+": "+v)
	}
	detail := ""
	if len(msgs) > 0 {
		detail = ": " + oneLine(strings.Join(msgs, "; "), 300)
	}
	e := rejectedIf(&Error{Status: resp.status, Message: fmt.Sprintf("%s answered HTTP %d%s", name, resp.status, detail)}, resp.status)
	switch {
	case resp.status == http.StatusUnauthorized || resp.status == http.StatusForbidden:
		e.Auth = true
		e.Message = fmt.Sprintf("%s refused the credentials (HTTP %d); check the account email and replace the API token", name, resp.status)
	case resp.status == http.StatusNotFound:
		e.NotFound = true
	case resp.status >= 300 && resp.status < 400:
		e.Message = fmt.Sprintf("%s answered with a redirect (HTTP %d); check the site URL", name, resp.status)
	}
	return e
}

type jiraUser struct {
	AccountID    string `json:"accountId"`
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
	AccountType  string `json:"accountType"`
	Active       *bool  `json:"active"`
}

func (u *jiraUser) person() *Person {
	if u == nil || u.AccountID == "" {
		return nil
	}
	return &Person{AccountID: u.AccountID, Name: u.DisplayName, Email: u.EmailAddress}
}

type jiraIssue struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Fields struct {
		Summary string `json:"summary"`
		Status  *struct {
			Name           string `json:"name"`
			StatusCategory struct {
				Key string `json:"key"`
			} `json:"statusCategory"`
		} `json:"status"`
		Assignee    *jiraUser                   `json:"assignee"`
		DueDate     string                      `json:"duedate"`
		Priority    *struct{ Name string }      `json:"priority"`
		Updated     string                      `json:"updated"`
		Parent      *struct{ Key string }       `json:"parent"`
		Labels      []string                    `json:"labels"`
		Project     *struct{ Key, Name string } `json:"project"`
		Description *adfNode                    `json:"description"`
	} `json:"fields"`
}

// jiraCanceled are done-category status names that mean the work was dropped rather than finished.
var jiraCanceled = map[string]bool{"canceled": true, "cancelled": true, "won't do": true, "wont do": true, "rejected": true, "duplicate": true}

func jiraCategory(key, name string) string {
	switch key {
	case "done":
		if jiraCanceled[strings.ToLower(strings.TrimSpace(name))] {
			return model.StatusCanceled
		}
		return model.StatusDone
	case "indeterminate":
		return model.StatusInProgress
	}
	return model.StatusToDo
}

func (j *jira) issue(raw jiraIssue) Issue {
	f := raw.Fields
	out := Issue{ID: raw.ID, Key: raw.Key, Title: f.Summary, URL: j.site + "/browse/" + url.PathEscape(raw.Key),
		Assignee: f.Assignee.person(), DueDate: f.DueDate, Labels: f.Labels, StatusCategory: model.StatusToDo}
	if out.Labels == nil {
		out.Labels = []string{}
	}
	if f.Status != nil {
		out.Status = f.Status.Name
		out.StatusCategory = jiraCategory(f.Status.StatusCategory.Key, f.Status.Name)
	}
	if f.Priority != nil {
		out.Priority = f.Priority.Name
	}
	if t, err := time.Parse(jiraTimeLayout, f.Updated); err == nil {
		out.UpdatedAt = t
	}
	if f.Parent != nil {
		out.Parent = f.Parent.Key
	}
	if f.Project != nil {
		out.Project = f.Project.Key
	}
	if f.Description != nil {
		out.Description = oneLineLimit(adfText(*f.Description), maxDescription)
	}
	return out
}

// oneLineLimit cuts text to n characters (keeping lines).
func oneLineLimit(text string, n int) string {
	if r := []rune(text); len(r) > n {
		return string(r[:n]) + "…"
	}
	return text
}

func (j *jira) Myself(ctx context.Context) (Person, error) {
	var u jiraUser
	if err := j.call(ctx, http.MethodGet, "/rest/api/3/myself", nil, nil, &u); err != nil {
		return Person{}, err
	}
	if p := u.person(); p != nil {
		return *p, nil
	}
	return Person{}, errorf("%s did not say which account the token belongs to", j.name)
}

type jiraProject struct {
	ID   string    `json:"id"`
	Key  string    `json:"key"`
	Name string    `json:"name"`
	Lead *jiraUser `json:"lead"`
}

func (p jiraProject) project() Project {
	return Project{ID: p.ID, Key: p.Key, Name: p.Name, Lead: p.Lead.person()}
}

func (j *jira) Projects(ctx context.Context, max int) ([]Project, error) {
	out := []Project{}
	for start, pages := 0, 0; len(out) < max && pages < maxJiraPages; pages++ {
		var page struct {
			IsLast bool          `json:"isLast"`
			Values []jiraProject `json:"values"`
		}
		q := url.Values{"startAt": {strconv.Itoa(start)}, "maxResults": {"50"}, "expand": {"lead"}}
		if err := j.call(ctx, http.MethodGet, "/rest/api/3/project/search", q, nil, &page); err != nil {
			return nil, err
		}
		for _, p := range page.Values {
			out = append(out, p.project())
		}
		if page.IsLast || len(page.Values) == 0 {
			break
		}
		start += len(page.Values)
	}
	return out[:min(len(out), max)], nil
}

func (j *jira) Project(ctx context.Context, key string) (Project, error) {
	var p jiraProject
	if err := j.call(ctx, http.MethodGet, "/rest/api/3/project/"+url.PathEscape(key), nil, nil, &p); err != nil {
		return Project{}, err
	}
	return p.project(), nil
}

func (j *jira) Issue(ctx context.Context, ref string) (Issue, error) {
	var raw jiraIssue
	q := url.Values{"fields": {jiraIssueFields + ",description"}}
	if err := j.call(ctx, http.MethodGet, "/rest/api/3/issue/"+url.PathEscape(ref), q, nil, &raw); err != nil {
		return Issue{}, err
	}
	return j.issue(raw), nil
}

// jql builds the search for a query. Keys are validated by callers; they are quoted all the same.
func jiraJQL(q Query) string {
	var clauses []string
	if q.Parent != nil {
		clauses = append(clauses, fmt.Sprintf("parent = %q", q.Parent.Key))
	} else {
		clauses = append(clauses, fmt.Sprintf("project = %q", q.Project))
	}
	if !q.IncludeDone {
		clauses = append(clauses, "statusCategory != Done")
	}
	if strings.TrimSpace(q.JQL) != "" {
		clauses = append(clauses, "("+q.JQL+")")
	}
	return strings.Join(clauses, " AND ")
}

// Search pages through /rest/api/3/search/jql with nextPageToken until isLast. Pages may hold fewer issues than
// asked for, so the loop never assumes a page size.
func (j *jira) Search(ctx context.Context, q Query) ([]Issue, error) {
	out := []Issue{}
	token := ""
	for page := 0; page < maxJiraPages; page++ {
		params := url.Values{"jql": {jiraJQL(q)}, "maxResults": {strconv.Itoa(jiraPageSize)}, "fields": {jiraIssueFields}}
		if token != "" {
			params.Set("nextPageToken", token)
		}
		var res struct {
			Issues        []jiraIssue `json:"issues"`
			NextPageToken string      `json:"nextPageToken"`
			IsLast        bool        `json:"isLast"`
		}
		if err := j.call(ctx, http.MethodGet, "/rest/api/3/search/jql", params, nil, &res); err != nil {
			return nil, err
		}
		for _, raw := range res.Issues {
			out = append(out, j.issue(raw))
		}
		if q.Max > 0 && len(out) > q.Max {
			return nil, &LimitError{Count: len(out), Max: q.Max}
		}
		if res.IsLast || res.NextPageToken == "" {
			return out, nil
		}
		token = res.NextPageToken
	}
	return nil, errorf("%s returned too many pages of issues", j.name)
}

type jiraComment struct {
	ID      string    `json:"id"`
	Author  *jiraUser `json:"author"`
	Created string    `json:"created"`
	Body    *adfNode  `json:"body"`
}

func (j *jira) Comments(ctx context.Context, ref string, limit int) ([]Comment, error) {
	var res struct {
		Comments []jiraComment `json:"comments"`
	}
	q := url.Values{"orderBy": {"-created"}, "maxResults": {strconv.Itoa(max(1, min(limit, maxCommentsPage)))}}
	if err := j.call(ctx, http.MethodGet, "/rest/api/3/issue/"+url.PathEscape(ref)+"/comment", q, nil, &res); err != nil {
		return nil, err
	}
	out := make([]Comment, 0, len(res.Comments))
	for _, c := range res.Comments {
		at, err := time.Parse(jiraTimeLayout, c.Created)
		if err != nil {
			continue
		}
		cm := Comment{ID: c.ID, At: at}
		if p := c.Author.person(); p != nil {
			cm.Author = *p
		}
		if c.Body != nil {
			cm.Body = adfText(*c.Body)
		}
		out = append(out, cm)
	}
	return newestFirst(out, limit), nil
}

func (j *jira) PostComment(ctx context.Context, ref, text string, mentions []Person) (Posted, error) {
	var res struct {
		ID string `json:"id"`
	}
	body := map[string]any{"body": adfDoc(text, mentions)}
	if err := j.call(ctx, http.MethodPost, "/rest/api/3/issue/"+url.PathEscape(ref)+"/comment", nil, body, &res); err != nil {
		return Posted{}, err
	}
	return Posted{ID: res.ID, URL: j.site + "/browse/" + url.PathEscape(ref) + "?focusedCommentId=" + url.QueryEscape(res.ID)}, nil
}

func (j *jira) CreateIssue(ctx context.Context, in NewIssue) (Issue, error) {
	fields := map[string]any{"project": map[string]string{"key": in.Project}, "summary": in.Title,
		"issuetype": map[string]string{"name": j.issueTyp}}
	if in.Description != "" {
		fields["description"] = adfDoc(in.Description, nil)
	}
	if in.Parent != "" {
		fields["parent"] = map[string]string{"key": in.Parent}
	}
	var res struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	if err := j.call(ctx, http.MethodPost, "/rest/api/3/issue", nil, map[string]any{"fields": fields}, &res); err != nil {
		return Issue{}, err
	}
	return Issue{ID: res.ID, Key: res.Key, Title: in.Title, Project: in.Project, URL: j.site + "/browse/" + url.PathEscape(res.Key),
		StatusCategory: model.StatusToDo, Labels: []string{}}, nil
}

// Users lists active people (not apps) through /rest/api/3/users/search.
func (j *jira) Users(ctx context.Context, max int) ([]Person, error) {
	out := []Person{}
	for start, pages := 0, 0; len(out) < max && pages < maxJiraPages; pages++ {
		var page []jiraUser
		q := url.Values{"startAt": {strconv.Itoa(start)}, "maxResults": {"100"}}
		if err := j.call(ctx, http.MethodGet, "/rest/api/3/users/search", q, nil, &page); err != nil {
			return nil, err
		}
		for _, u := range page {
			if p := u.person(); p != nil && u.AccountType == "atlassian" && (u.Active == nil || *u.Active) {
				out = append(out, *p)
			}
		}
		if len(page) == 0 {
			break
		}
		start += len(page)
	}
	return out[:min(len(out), max)], nil
}

func (j *jira) User(ctx context.Context, accountID string) (Person, error) {
	var u jiraUser
	if err := j.call(ctx, http.MethodGet, "/rest/api/3/user", url.Values{"accountId": {accountID}}, nil, &u); err != nil {
		return Person{}, err
	}
	if p := u.person(); p != nil {
		return *p, nil
	}
	return Person{}, &Error{Message: "no such person", NotFound: true}
}
