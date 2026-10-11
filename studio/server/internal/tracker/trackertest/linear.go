package trackertest

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Linear serves the GraphQL operations the client sends, recognised by their root fields. The API key goes in
// Authorization as it is, as Linear requires for personal keys.
func Linear(d *Data, apiKey string) http.Handler {
	return guard(d, apiKey, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, 400, map[string]any{"errors": []any{map[string]any{"message": "bad request"}}})
			return
		}
		data, errMsg := linearOperation(d, req.Query, req.Variables)
		if errMsg != "" {
			writeJSON(w, 200, map[string]any{"data": nil, "errors": []any{map[string]any{"message": errMsg,
				"extensions": map[string]any{"code": "INVALID_INPUT", "type": "invalid input"}}}})
			return
		}
		writeJSON(w, 200, map[string]any{"data": data})
	}))
}

// LinearRateLimited is the answer Linear gives when a key is over its limit (HTTP 400, code RATELIMITED).
func LinearRateLimited(w http.ResponseWriter) {
	w.Header().Set("X-RateLimit-Requests-Reset", strconv.FormatInt(time.Now().UnixMilli(), 10))
	writeJSON(w, 400, map[string]any{"errors": []any{map[string]any{"message": "Rate limit exceeded",
		"extensions": map[string]any{"code": "RATELIMITED"}}}})
}

func linearOperation(d *Data, q string, vars map[string]any) (any, string) {
	switch {
	case strings.Contains(q, "viewer"):
		return map[string]any{"viewer": linearUser(&d.Me)}, ""
	case strings.Contains(q, "teams(filter"):
		key, _ := vars["key"].(string)
		nodes := []any{}
		if _, ok := d.Projects[key]; ok {
			nodes = append(nodes, map[string]any{"id": "team-" + key, "key": key, "name": "Team " + key})
		}
		return map[string]any{"teams": map[string]any{"nodes": nodes}}, ""
	case strings.Contains(q, "teams(first"):
		nodes := []any{}
		for _, key := range sortedKeys(d.Projects) {
			nodes = append(nodes, map[string]any{"id": "team-" + key, "key": key, "name": "Team " + key})
		}
		return map[string]any{"teams": map[string]any{"nodes": nodes, "pageInfo": map[string]any{"hasNextPage": false}}}, ""
	case strings.Contains(q, "commentCreate"):
		input, _ := vars["input"].(map[string]any)
		id, _ := input["issueId"].(string)
		if _, ok := d.Issues[id]; !ok {
			return nil, "Entity not found: Issue"
		}
		d.Posted = append(d.Posted, Posted{Issue: id, Body: input["body"]})
		return map[string]any{"commentCreate": map[string]any{"success": true,
			"comment": map[string]any{"id": "lc" + strconv.Itoa(len(d.Posted)), "url": "https://linear.app/acme/issue/" + id}}}, ""
	case strings.Contains(q, "issueCreate"):
		input, _ := vars["input"].(map[string]any)
		d.Created = append(d.Created, input)
		key := "NEW-" + strconv.Itoa(len(d.Created))
		title, _ := input["title"].(string)
		return map[string]any{"issueCreate": map[string]any{"success": true, "issue": linearIssue(d, &Issue{ID: "new", Key: key, Title: title,
			Category: "new", Status: "Backlog", Project: "NEW", Updated: time.Now()}, nil)}}, ""
	case strings.Contains(q, "users(first"):
		nodes := []any{}
		for _, u := range d.Users {
			nodes = append(nodes, linearUser(&u))
		}
		return map[string]any{"users": map[string]any{"nodes": nodes, "pageInfo": map[string]any{"hasNextPage": false}}}, ""
	case strings.Contains(q, "user(id"):
		for _, u := range append(d.Users, d.Me) {
			if u.ID == vars["id"] {
				return map[string]any{"user": linearUser(&u)}, ""
			}
		}
		return nil, "Entity not found: User"
	case strings.Contains(q, "issues(filter"):
		return linearSearch(d, q, vars), ""
	case strings.Contains(q, "issue(id"):
		id, _ := vars["id"].(string)
		i, ok := d.Issues[id]
		if !ok {
			return nil, "Entity not found: Issue"
		}
		if strings.Contains(q, "issue(id: $id) { comments(") {
			return map[string]any{"issue": map[string]any{"comments": map[string]any{"nodes": linearComments(i, nil)}}}, ""
		}
		out := linearIssue(d, i, nil)
		out["description"] = "About " + i.Title
		return map[string]any{"issue": out}, ""
	}
	return nil, "unknown operation"
}

func linearUser(u *User) map[string]any {
	if u == nil {
		return nil
	}
	return map[string]any{"id": u.ID, "name": u.Name, "displayName": strings.ToLower(u.Name), "email": u.Email,
		"url": "https://linear.app/acme/profiles/" + strings.ToLower(strings.ReplaceAll(u.Name, " ", "")), "active": true}
}

func linearState(i *Issue) map[string]any {
	typ := map[string]string{"new": "unstarted", "indeterminate": "started", "done": "completed"}[i.Category]
	if strings.EqualFold(i.Status, "canceled") {
		typ = "canceled"
	}
	return map[string]any{"name": i.Status, "type": typ}
}

func linearComments(i *Issue, since *time.Time) []any {
	out := []any{}
	for _, c := range i.Comments {
		if since != nil && !c.At.After(*since) {
			continue
		}
		out = append(out, map[string]any{"id": c.ID, "body": c.Body, "createdAt": c.At.UTC().Format(time.RFC3339Nano), "user": linearUser(&c.Author)})
	}
	return out
}

func linearIssue(d *Data, i *Issue, since *time.Time) map[string]any {
	labels := []any{}
	for _, l := range i.Labels {
		labels = append(labels, map[string]any{"name": l})
	}
	out := map[string]any{"id": i.ID, "identifier": i.Key, "title": i.Title, "url": "https://linear.app/acme/issue/" + i.Key,
		"dueDate": nilIfEmpty(i.Due), "priorityLabel": i.Priority, "updatedAt": i.Updated.UTC().Format(time.RFC3339Nano),
		"state": linearState(i), "assignee": linearUser(i.Assignee), "team": map[string]any{"key": i.Project},
		"labels": map[string]any{"nodes": labels}}
	if i.Parent != "" {
		out["parent"] = map[string]any{"identifier": i.Parent}
	}
	if lead := d.Projects[i.Project]; lead != nil {
		out["project"] = map[string]any{"lead": linearUser(lead)}
	}
	if since != nil {
		out["comments"] = map[string]any{"nodes": linearComments(i, since)}
	}
	return out
}

func linearSearch(d *Data, q string, vars map[string]any) any {
	filter, _ := vars["filter"].(map[string]any)
	teamKey := dig(filter, "team", "key", "eq")
	parentID := dig(filter, "parent", "id", "eq")
	_, openOnly := filter["state"]
	matches := d.sortedIssues(func(i *Issue) bool {
		switch {
		case openOnly && i.Category == "done":
			return false
		case parentID != "":
			p := d.Issues[i.Parent]
			return p != nil && p.ID == parentID
		}
		return i.Project == teamKey
	})
	var since *time.Time
	if s, ok := vars["since"].(string); ok && strings.Contains(q, "comments(first") {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			since = &t
		}
	}
	start := 0
	if after, ok := vars["after"].(string); ok {
		start, _ = strconv.Atoi(after)
	}
	end := min(start+d.PageSize, len(matches))
	nodes := []any{}
	for _, i := range matches[start:end] {
		nodes = append(nodes, linearIssue(d, i, since))
	}
	return map[string]any{"issues": map[string]any{"nodes": nodes,
		"pageInfo": map[string]any{"hasNextPage": end < len(matches), "endCursor": strconv.Itoa(end)}}}
}

func dig(m map[string]any, keys ...string) string {
	var cur any = m
	for _, k := range keys {
		next, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = next[k]
	}
	s, _ := cur.(string)
	return s
}
