package api

import (
	"net/http"
	"testing"
)

func TestDeliverReportCadence(t *testing.T) {
	e := newReportEnv(t)
	sr := e.scheduledRun(t)
	e.destinations(t, sr)
	r := e.call(t, sr, "POST", "/reports", map[string]any{"project": "Atlas", "markdown": "Today", "cadence": "hourly"})
	wantStatus(t, r, http.StatusBadRequest)
	if r.body.Error.Code != "bad_cadence" {
		t.Errorf("bad cadence = %+v", r.body.Error)
	}
	r = e.call(t, sr, "POST", "/reports", map[string]any{"project": "Atlas", "markdown": "Today", "cadence": "Daily"})
	wantStatus(t, r, http.StatusCreated)
	daily := decodeData[reportResult](t, r)
	r = e.call(t, sr, "POST", "/reports", map[string]any{"project": "Atlas", "markdown": "This week"})
	wantStatus(t, r, http.StatusCreated)
	weekly := decodeData[reportResult](t, r)
	if daily.Cadence != "daily" || len(daily.Period) != len("2026-10-06") || weekly.Cadence != "weekly" || weekly.Period != weekly.Week {
		t.Errorf("results = %+v / %+v", daily, weekly)
	}
	view := decodeData[workOrderReportsView](t, e.do(t, "GET", "/api/work-orders/"+*sr.run.WorkOrderID+"/reports", nil))
	if len(view.Reports) != 2 || view.Reports[0].Cadence != "daily" || view.Reports[0].Period != daily.Period || view.Reports[1].Cadence != "weekly" {
		t.Errorf("work order reports = %+v", view.Reports)
	}
}
