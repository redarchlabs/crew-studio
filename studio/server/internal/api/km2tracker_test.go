package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"crewstudio/internal/connections"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

const km2TrackerKey = "km2_tracker-api-key-secret"

func (e trackerEnv) km2Connection(t *testing.T) model.Connection {
	t.Helper()
	r := e.do(t, "POST", "/api/connections", map[string]any{"name": "Acme KM2", "kind": model.TrackerKM2, "area_ids": []string{financeArea},
		"config":  map[string]string{"base_url": e.km2, "self_email": "bot@example.com", "issue_url_template": "https://km2.example.com/r?record_id={id}"},
		"secrets": map[string]string{"api_key": km2TrackerKey}})
	wantStatus(t, r, http.StatusCreated)
	return decodeData[model.Connection](t, r)
}

func TestKM2TrackerConnectionIsCheckedAndTested(t *testing.T) {
	e := newTrackerEnv(t)
	for want, body := range map[string]map[string]any{
		"config.base_url: must be https": {"config": map[string]string{"base_url": "http://km2.example.com", "self_email": "bot@example.com"},
			"secrets": map[string]string{"api_key": km2TrackerKey}},
		"config.base_url: the KM2 base URL must be an https address": {"config": map[string]string{"base_url": "https://km2.example.com/api", "self_email": "bot@example.com"},
			"secrets": map[string]string{"api_key": km2TrackerKey}},
		"secrets.api_key: must start with km2_": {"config": map[string]string{"base_url": e.km2, "self_email": "bot@example.com"},
			"secrets": map[string]string{"api_key": "lin_api_x"}},
	} {
		body["name"], body["kind"], body["area_ids"] = "Bad KM2", model.TrackerKM2, []string{financeArea}
		r := e.do(t, "POST", "/api/connections", body)
		wantStatus(t, r, http.StatusUnprocessableEntity)
		if got := strings.Join(r.body.Error.Details, "; "); !strings.Contains(got, want) {
			t.Errorf("details %q lack %q", got, want)
		}
	}
	conn := e.km2Connection(t)
	res := decodeData[connections.TestResult](t, e.do(t, "POST", "/api/connections/"+conn.ID+"/test", map[string]any{}))
	if !res.OK || res.Message != "Connected as Studio Bot. The organisation has the tracker entities." {
		t.Errorf("test = %+v", res)
	}
	sync := decodeData[peopleSyncResponse](t, e.do(t, "POST", "/api/connections/"+conn.ID+"/people/sync", map[string]any{}))
	if sync.Read != 2 || len(sync.People) != 2 {
		t.Errorf("sync = %+v", sync)
	}
}

func TestRunTrackerOnKM2(t *testing.T) {
	e := newTrackerEnv(t)
	sr := e.scheduledRun(t)
	conn := e.km2Connection(t)
	if err := e.st.SetCrewConnections(context.Background(), sr.crew.ID, []string{conn.ID}, store.AllAreas); err != nil {
		t.Fatal(err)
	}
	issue := decodeData[issueView](t, e.call(t, sr, "GET", "/trackers/issues/km2_tracker:prj-11", nil))
	if issue.Key != "PRJ-11" || issue.Connection != "Acme KM2" || len(issue.Comments) != 1 || issue.Assignee == nil {
		t.Errorf("issue = %+v", issue)
	}
	found := decodeData[foundIssues](t, e.call(t, sr, "GET", "/trackers/issues?project=prj&blocked=true", nil))
	if found.Total != 1 || found.Issues[0].Key != "PRJ-11" {
		t.Errorf("found = %+v", found)
	}
	// The project lead (from the trk_project record) may be mentioned without a people map, and is named in the text
	r := e.call(t, sr, "POST", "/trackers/issues/PRJ-11/comments", map[string]any{"text": "Escalating", "mentions": []string{"acc-dana"}})
	wantStatus(t, r, http.StatusCreated)
	res := decodeData[postResult](t, r)
	if !res.Posted || res.Logged == nil || res.Logged.Subject != "km2_tracker:PRJ-11" || !strings.HasPrefix(res.URL, "https://km2.example.com/r?record_id=") {
		t.Fatalf("comment = %+v", res)
	}
	posted := e.data.PostedComments()
	if len(posted) != 1 || !strings.Contains(toJSONString(posted[0].Body), `"body":"@Dana Ruiz\n\nEscalating"`) {
		t.Fatalf("posted = %+v", posted)
	}
}
