package trackertest

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

const jiraTime = "2006-01-02T15:04:05.000-0700"

var (
	jqlProject = regexp.MustCompile(`project = "([A-Z0-9_]+)"`)
	jqlParent  = regexp.MustCompile(`parent = "([A-Z0-9_]+-[0-9]+)"`)
)

// Jira serves the Jira Cloud REST v3 endpoints the client uses. Every request must carry Basic auth for
// email:token, as Jira requires.
func Jira(d *Data, email, token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rest/api/3/myself", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, jiraUser(&d.Me)) })
	mux.HandleFunc("GET /rest/api/3/project/search", func(w http.ResponseWriter, _ *http.Request) {
		values := []any{}
		for _, key := range sortedKeys(d.Projects) {
			values = append(values, map[string]any{"id": "p-" + key, "key": key, "name": "Project " + key, "lead": jiraUser(d.Projects[key])})
		}
		writeJSON(w, 200, map[string]any{"isLast": true, "values": values})
	})
	mux.HandleFunc("GET /rest/api/3/project/{key}", func(w http.ResponseWriter, r *http.Request) {
		lead, ok := d.Projects[r.PathValue("key")]
		if !ok {
			writeJSON(w, 404, map[string]any{"errorMessages": []string{"No project could be found with key '" + r.PathValue("key") + "'."}})
			return
		}
		writeJSON(w, 200, map[string]any{"id": "p", "key": r.PathValue("key"), "name": "Project", "lead": jiraUser(lead)})
	})
	mux.HandleFunc("GET /rest/api/3/issue/{key}", func(w http.ResponseWriter, r *http.Request) {
		i, ok := d.Issues[r.PathValue("key")]
		if !ok {
			writeJSON(w, 404, map[string]any{"errorMessages": []string{"Issue does not exist or you do not have permission to see it."}})
			return
		}
		writeJSON(w, 200, jiraIssue(i))
	})
	mux.HandleFunc("GET /rest/api/3/search/jql", func(w http.ResponseWriter, r *http.Request) { jiraSearch(d, w, r) })
	mux.HandleFunc("GET /rest/api/3/issue/{key}/comment", func(w http.ResponseWriter, r *http.Request) {
		i, ok := d.Issues[r.PathValue("key")]
		if !ok {
			writeJSON(w, 404, map[string]any{"errorMessages": []string{"Issue does not exist"}})
			return
		}
		comments := []any{}
		for n := len(i.Comments) - 1; n >= 0; n-- { // orderBy=-created
			c := i.Comments[n]
			comments = append(comments, map[string]any{"id": c.ID, "author": jiraUser(&c.Author), "created": c.At.Format(jiraTime),
				"body": map[string]any{"type": "doc", "version": 1, "content": []any{map[string]any{"type": "paragraph",
					"content": []any{map[string]any{"type": "text", "text": c.Body}}}}}})
		}
		writeJSON(w, 200, map[string]any{"startAt": 0, "total": len(comments), "comments": comments})
	})
	mux.HandleFunc("POST /rest/api/3/issue/{key}/comment", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		if json.Unmarshal(raw, &body) != nil || body["body"] == nil {
			writeJSON(w, 400, map[string]any{"errorMessages": []string{"bad body"}})
			return
		}
		d.Posted = append(d.Posted, Posted{Issue: r.PathValue("key"), Body: body["body"]})
		writeJSON(w, 201, map[string]any{"id": "c" + strconv.Itoa(len(d.Posted))})
	})
	mux.HandleFunc("POST /rest/api/3/issue", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		d.Created = append(d.Created, body)
		writeJSON(w, 201, map[string]any{"id": "20001", "key": "NEW-" + strconv.Itoa(len(d.Created))})
	})
	mux.HandleFunc("GET /rest/api/3/users/search", func(w http.ResponseWriter, r *http.Request) {
		out := []any{}
		if r.URL.Query().Get("startAt") == "0" {
			for _, u := range d.Users {
				m := jiraUser(&u)
				m["accountType"] = "atlassian"
				out = append(out, m)
			}
			out = append(out, map[string]any{"accountId": "app-1", "displayName": "Some App", "accountType": "app"})
		}
		writeJSON(w, 200, out)
	})
	mux.HandleFunc("GET /rest/api/3/user", func(w http.ResponseWriter, r *http.Request) {
		for _, u := range append(d.Users, d.Me) {
			if u.ID == r.URL.Query().Get("accountId") {
				writeJSON(w, 200, jiraUser(&u))
				return
			}
		}
		writeJSON(w, 404, map[string]any{"errorMessages": []string{"user not found"}})
	})
	return guard(d, "Basic "+basic(email, token), mux)
}

func basic(email, token string) string {
	return base64Std(email + ":" + token)
}

// guard checks auth, records the request and applies Fail.
func guard(d *Data, auth string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.Requests = append(d.Requests, r.Method+" "+r.URL.Path)
		if d.failing(w) {
			return
		}
		if r.Header.Get("Authorization") != auth {
			writeJSON(w, 401, map[string]any{"errorMessages": []string{"unauthorized"}})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func jiraUser(u *User) map[string]any {
	if u == nil {
		return nil
	}
	m := map[string]any{"accountId": u.ID, "displayName": u.Name, "active": true}
	if u.Email != "" {
		m["emailAddress"] = u.Email
	}
	return m
}

func jiraIssue(i *Issue) map[string]any {
	fields := map[string]any{
		"summary":  i.Title,
		"status":   map[string]any{"name": i.Status, "statusCategory": map[string]any{"key": i.Category}},
		"assignee": jiraUser(i.Assignee),
		"duedate":  nilIfEmpty(i.Due),
		"priority": map[string]any{"name": i.Priority},
		"updated":  i.Updated.Format(jiraTime),
		"labels":   append([]string{}, i.Labels...),
		"project":  map[string]any{"key": i.Project, "name": "Project " + i.Project},
		"description": map[string]any{"type": "doc", "version": 1, "content": []any{map[string]any{"type": "paragraph",
			"content": []any{map[string]any{"type": "text", "text": "About " + i.Title}}}}},
	}
	if i.Parent != "" {
		fields["parent"] = map[string]any{"key": i.Parent}
	}
	return map[string]any{"id": i.ID, "key": i.Key, "fields": fields}
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func jiraSearch(d *Data, w http.ResponseWriter, r *http.Request) {
	jql := r.URL.Query().Get("jql")
	project, parent := jqlProject.FindStringSubmatch(jql), jqlParent.FindStringSubmatch(jql)
	openOnly := strings.Contains(jql, "statusCategory != Done")
	matches := d.sortedIssues(func(i *Issue) bool {
		switch {
		case openOnly && i.Category == "done":
			return false
		case parent != nil:
			return i.Parent == parent[1]
		case project != nil:
			return i.Project == project[1]
		}
		return false
	})
	start, _ := strconv.Atoi(r.URL.Query().Get("nextPageToken"))
	end := min(start+d.PageSize, len(matches))
	issues := []any{}
	for _, i := range matches[start:end] {
		issues = append(issues, jiraIssue(i))
	}
	res := map[string]any{"issues": issues, "isLast": end >= len(matches)}
	if end < len(matches) {
		res["nextPageToken"] = strconv.Itoa(end)
	}
	writeJSON(w, 200, res)
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}
