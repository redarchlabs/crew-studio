package store

import (
	"testing"
	"time"

	"crewstudio/internal/model"
)

func TestTrackingItemsCarryTheLeadNameFromThePeopleMap(t *testing.T) {
	f := newFixture(t)
	a := f.area("Alpha")
	crew := f.simpleCrew("tracker crew", a)
	jira := must(f.s.CreateConnection(bg(), "99999999-0000-4000-8000-0000000000c1", model.ConnectionInput{Name: "jira", Kind: "jira", AreaIDs: []string{a}}, nil))
	other := must(f.s.CreateConnection(bg(), "99999999-0000-4000-8000-0000000000c2", model.ConnectionInput{Name: "jira-2", Kind: "jira", AreaIDs: []string{a}}, nil))
	_, err := f.s.SeeTrackerPeople(bg(), jira.ID, []model.TrackerPerson{{AccountID: "acc-dana", DisplayName: "Dana Ruiz"}}, time.Now())
	mustOK(t, err)
	_, err = f.s.SeeTrackerPeople(bg(), other.ID, []model.TrackerPerson{{AccountID: "acc-sam", DisplayName: "Sam Lee"}}, time.Now())
	mustOK(t, err)

	add := func(ref, lead string) model.TrackingItem {
		return must(f.s.AddTrackingItem(bg(), crew.ID, model.TrackingItemInput{ConnectionID: jira.ID, Project: "PRJ", ItemRef: ref,
			IncludeChildren: "all", LeadAccountID: lead}, nil))
	}
	if named := add("PRJ-1", "acc-dana"); named.LeadName != "Dana Ruiz" {
		t.Errorf("mapped lead = %q", named.LeadName)
	}
	// Not in this connection's map (only another connection's), and no lead at all
	if unmapped := add("PRJ-2", "acc-sam"); unmapped.LeadName != "" || unmapped.LeadAccountID != "acc-sam" {
		t.Errorf("unmapped lead = %+v", unmapped)
	}
	if none := add("PRJ-3", ""); none.LeadName != "" {
		t.Errorf("no lead = %q", none.LeadName)
	}
	names := map[string]string{}
	for _, it := range must(f.s.ListTrackingItems(bg(), TrackingFilter{CrewID: crew.ID})) {
		names[it.ItemRef] = it.LeadName
	}
	if len(names) != 3 || names["PRJ-1"] != "Dana Ruiz" || names["PRJ-2"] != "" || names["PRJ-3"] != "" {
		t.Errorf("listed lead names = %v", names)
	}
}
