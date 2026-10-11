package api

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"crewstudio/internal/auth"
	"crewstudio/internal/km2"
	"crewstudio/internal/km2/km2test"
	"crewstudio/internal/store"
)

func TestKM2ConnectionFoldersForPickers(t *testing.T) {
	e := newKM2Env(t)
	conn := e.km2Connection(t, "KM2 Finance")
	r := e.do(t, "GET", "/api/connections/"+conn.ID+"/km2/folders", nil)
	wantStatus(t, r, http.StatusOK)
	list := decodeData[km2.FolderList](t, r)
	if !list.OK || list.FolderCount != 3 || len(list.Folders) != 3 || list.Folders[1].Path != "Projects.Atlas" ||
		list.Allowed.DefaultFolderID != km2test.FolderAtlas || !slices.Equal(list.Allowed.WriteFolderIDs, []string{km2test.FolderProjects}) {
		t.Fatalf("folders = %+v", list)
	}
	if strings.Contains(string(r.body.Data), km2TestKey) {
		t.Error("the key leaked into the answer")
	}

	// A key that may not list folders, and a KM2 that fails, give a message the form can show
	e.data.SetScopes("search:read", "knowledge:write")
	list = decodeData[km2.FolderList](t, e.do(t, "GET", "/api/connections/"+conn.ID+"/km2/folders", nil))
	if list.OK || list.StatusCode != 403 || !strings.Contains(list.Message, "knowledge:read") || len(list.Folders) != 0 {
		t.Errorf("no knowledge:read = %+v", list)
	}
	e.data.SetScopes("search:read", "knowledge:read", "knowledge:write")
	e.data.Fail("/api/v1/knowledge/folders", km2test.Failure{Status: 502, Detail: "upstream down", Times: 1})
	list = decodeData[km2.FolderList](t, e.do(t, "GET", "/api/connections/"+conn.ID+"/km2/folders", nil))
	if list.OK || !strings.HasPrefix(list.Message, "Could not use the connection:") || strings.Contains(list.Message, km2TestKey) {
		t.Errorf("KM2 failure = %+v", list)
	}

	// Not a KM2 connection, outside the person's areas, or without a permission that needs folders
	httpConn := e.do(t, "POST", "/api/connections", httpConnBody("api", "https://api.example"))
	wantStatus(t, httpConn, http.StatusCreated)
	wantStatus(t, e.do(t, "GET", "/api/connections/"+decodeData[struct{ ID string }](t, httpConn).ID+"/km2/folders", nil), http.StatusUnprocessableEntity)
	_, itTok := e.person(t, "it@example.com", []string{builderRole}, []string{itArea}, nil)
	wantStatus(t, e.as(itTok).do(t, "GET", "/api/connections/"+conn.ID+"/km2/folders", nil), http.StatusNotFound)
	_, plainTok := e.person(t, "plain@example.com", []string{viewerRole}, []string{financeArea}, nil)
	wantStatus(t, e.as(plainTok).do(t, "GET", "/api/connections/"+conn.ID+"/km2/folders", nil), http.StatusForbidden)
	// Someone who manages schedules in the area but cannot view connections still gets the list
	_, schedTok := e.person(t, "sched@example.com", nil, []string{financeArea}, []string{auth.SchedulesView, auth.SchedulesManage})
	if l := decodeData[km2.FolderList](t, e.as(schedTok).do(t, "GET", "/api/connections/"+conn.ID+"/km2/folders", nil)); !l.OK {
		t.Errorf("schedule manager = %+v", l)
	}
}

func TestKM2ConnectionFoldersAreRateLimited(t *testing.T) {
	e := newKM2Env(t)
	conn := e.km2Connection(t, "KM2 Finance")
	limited := false
	for i := 0; i < 40 && !limited; i++ {
		limited = e.do(t, "GET", "/api/connections/"+conn.ID+"/km2/folders", nil).status == http.StatusTooManyRequests
	}
	if !limited {
		t.Error("folder lists are not rate limited")
	}
}

func TestPromoteFoldersComeFromTheCrewsKM2Connection(t *testing.T) {
	e := newKM2Env(t)
	sr := e.scheduledRun(t)
	r := e.do(t, "GET", "/api/crews/"+sr.crew.ID+"/memory/promote/folders", nil)
	wantStatus(t, r, http.StatusConflict)
	conn := e.km2Connection(t, "KM2 Finance")
	if err := e.st.SetCrewConnections(context.Background(), sr.crew.ID, []string{conn.ID}, store.AllAreas); err != nil {
		t.Fatal(err)
	}
	list := decodeData[km2.FolderList](t, e.do(t, "GET", "/api/crews/"+sr.crew.ID+"/memory/promote/folders", nil))
	if !list.OK || list.Allowed.DefaultFolderID != km2test.FolderAtlas || len(list.Folders) != 3 {
		t.Errorf("promote folders = %+v", list)
	}
	_, viewTok := e.person(t, "view@example.com", []string{viewerRole}, []string{financeArea}, nil)
	wantStatus(t, e.as(viewTok).do(t, "GET", "/api/crews/"+sr.crew.ID+"/memory/promote/folders", nil), http.StatusForbidden)
}
