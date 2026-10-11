package chat

import (
	"context"
	"slices"
	"testing"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

func TestKM2ToolKeysNeedStateView(t *testing.T) {
	if got := KM2ToolKeys(&auth.Principal{Permissions: map[string]bool{}}); len(got) != 0 {
		t.Errorf("no permissions = %v", got)
	}
	if got := KM2ToolKeys(&auth.Principal{Permissions: map[string]bool{auth.StateView: true}}); !slices.Equal(got, []string{"km_search", "km_ask"}) {
		t.Errorf("state.view = %v", got)
	}
	// The KM2 tools only ever come from withKM2Tools, which checks the crew's connections
	if got := ToolKeysForCrew(&auth.Principal{Permissions: map[string]bool{auth.StateView: true}}, true); slices.Contains(got, "km_search") {
		t.Errorf("tracker crew = %v", got)
	}
}

func TestChatTurnsGetKM2ToolsOnlyWithAKM2Connection(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.turnOn(t, nil, 1)
	tools := func(text string) []string {
		t.Helper()
		turn, err := e.svc.StartConversation(ctx, e.ann, e.crew.ID, text)
		if err != nil {
			t.Fatal(err)
		}
		spec, err := e.st.GetRunSpec(ctx, turn.Run.ID)
		if err != nil {
			t.Fatal(err)
		}
		return spec.Chat.Tools
	}
	if got := tools("hi"); slices.Contains(got, "km_search") || slices.Contains(got, "km_ask") {
		t.Errorf("no KM2 connection: %v", got)
	}

	conn, err := e.st.CreateConnection(ctx, auth.NewUUID(), model.ConnectionInput{Name: "KM2 Finance", Kind: model.KindKM2,
		AreaIDs: []string{financeArea}, Config: map[string]string{"base_url": "https://km2.example"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.SetCrewConnections(ctx, e.crew.ID, []string{conn.ID}, store.AllAreas); err != nil {
		t.Fatal(err)
	}
	if got := tools("what do we know about Atlas?"); !slices.Contains(got, "km_search") || !slices.Contains(got, "km_ask") ||
		slices.Contains(got, "km_save_document") {
		t.Errorf("with KM2: %v", got)
	}

	off := false
	in := model.ConnectionInput{Name: conn.Name, Kind: model.KindKM2, AreaIDs: conn.AreaIDs, Config: conn.Config, Enabled: &off}
	if _, err := e.st.UpdateConnection(ctx, conn.ID, in, false); err != nil {
		t.Fatal(err)
	}
	if got := tools("again"); slices.Contains(got, "km_search") {
		t.Errorf("disabled KM2 connection: %v", got)
	}
}
