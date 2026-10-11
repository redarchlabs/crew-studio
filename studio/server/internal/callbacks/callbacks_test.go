package callbacks

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"crewstudio/internal/auth"
	"crewstudio/internal/model"
	"crewstudio/internal/storage"
	"crewstudio/internal/store"
	"crewstudio/internal/testdb"
)

func TestPublicIP(t *testing.T) {
	for ip, want := range map[string]bool{
		"8.8.8.8": true, "2606:4700::1111": true, "127.0.0.1": false, "10.1.2.3": false, "172.16.0.1": false, "192.168.1.1": false,
		"169.254.169.254": false, "100.64.0.1": false, "::1": false, "fd00::1": false, "fe80::1": false, "0.0.0.0": false,
		"::ffff:127.0.0.1": false, "2002:7f00:1::1": false, "2001:0:4136:e378::1": false, "192.0.2.10": false,
	} {
		if got := PublicIP(net.ParseIP(ip)); got != want {
			t.Errorf("PublicIP(%s) = %v", ip, got)
		}
	}
}

func TestGuardedDialRefusesPrivateAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	addr := srv.Listener.Addr().String()
	if _, err := guardedDial(false)(context.Background(), "tcp", addr); err == nil {
		t.Error("loopback must be refused")
	}
	conn, err := guardedDial(true)(context.Background(), "tcp", addr)
	if err != nil {
		t.Fatalf("allow-listed host should connect: %v", err)
	}
	conn.Close()
	if _, err := guardedDial(true)(context.Background(), "tcp", "169.254.169.254:80"); err == nil {
		t.Error("metadata address must be refused even for allow-listed hosts")
	}
}

func TestSign(t *testing.T) {
	sig := Sign("secret", 1700000000, []byte(`{"a":1}`))
	if sig != Sign("secret", 1700000000, []byte(`{"a":1}`)) || sig == Sign("other", 1700000000, []byte(`{"a":1}`)) || len(sig) != 71 {
		t.Errorf("signature = %s", sig)
	}
}

func TestDeliveryLifecycle(t *testing.T) {
	st := store.New(testdb.New(t, "callbacks_test"))
	ctx := context.Background()
	crews, _ := st.ListCrews(ctx, store.AllAreas)
	sa, err := st.CreateUser(ctx, model.UserInput{Kind: auth.KindService, Name: "erp", Status: model.UserActive})
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := st.SigningSecret(ctx, sa.ID)

	var calls atomic.Int32
	var fail atomic.Bool
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, _ := io.ReadAll(r.Body)
		ts, _ := strconv.ParseInt(r.Header.Get("X-CrewStudio-Timestamp"), 10, 64)
		if r.Header.Get("X-CrewStudio-Signature") != Sign(secret, ts, body) {
			t.Error("bad signature")
		}
		var p payload
		if json.Unmarshal(body, &p) != nil || p.Event != "run.finished" || p.Run.Status != model.RunSucceeded {
			t.Errorf("payload = %s", body)
		}
		if fail.Load() {
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer receiver.Close()
	u, _ := url.Parse(receiver.URL)
	if _, err := st.UpdateAccessSettings(ctx, model.AccessSettings{CallbackAllowedHosts: []string{u.Hostname()}}); err != nil {
		t.Fatal(err)
	}

	run, _, err := st.CreateRun(ctx, model.RunSpec{Crew: crews[0], BudgetUSD: 1}, store.RunMeta{
		Callback: &store.Callback{URL: receiver.URL + "/hook", SecretSource: store.CallbackFromServiceAccount, SecretOwner: sa.ID}})
	if err != nil {
		t.Fatal(err)
	}
	d := New(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.Tick(ctx)
	if calls.Load() != 0 {
		t.Fatal("nothing is sent before the run finishes")
	}
	out := "memo"
	if _, err := st.FinishRun(ctx, run.ID, store.RunResult{Status: model.RunSucceeded, Output: &out}); err != nil {
		t.Fatal(err)
	}

	fail.Store(true)
	d.Tick(ctx)
	deliveries, _ := st.ListDeliveries(ctx, run.ID)
	if calls.Load() != 1 || deliveries[0].Status != "pending" || deliveries[0].Attempts != 1 || *deliveries[0].LastStatusCode != 502 {
		t.Fatalf("after failure: calls %d, %+v", calls.Load(), deliveries[0])
	}
	d.Tick(ctx) // not due yet (30 s backoff)
	if calls.Load() != 1 {
		t.Error("retry must wait for its backoff")
	}
	if _, err := st.Pool().Exec(ctx, `UPDATE callback_deliveries SET next_attempt_at = now()`); err != nil {
		t.Fatal(err)
	}
	fail.Store(false)
	d.now = func() time.Time { return time.Now() }
	d.Tick(ctx)
	deliveries, _ = st.ListDeliveries(ctx, run.ID)
	if calls.Load() != 2 || deliveries[0].Status != "delivered" || deliveries[0].DeliveredAt == nil {
		t.Errorf("after retry: calls %d, %+v", calls.Load(), deliveries[0])
	}

	// A host that is not allow-listed and resolves to loopback is refused, then eventually marked failed
	run2, _, _ := st.CreateRun(ctx, model.RunSpec{Crew: crews[0], BudgetUSD: 1}, store.RunMeta{
		Callback: &store.Callback{URL: "https://localhost:1/x", SecretSource: store.CallbackFromServiceAccount, SecretOwner: sa.ID}})
	_, _ = st.FinishRun(ctx, run2.ID, store.RunResult{Status: model.RunFailed})
	for i := 0; i <= len(retryDelays); i++ {
		_, _ = st.Pool().Exec(ctx, `UPDATE callback_deliveries SET next_attempt_at = now() WHERE run_id = $1`, run2.ID)
		d.Tick(ctx)
	}
	deliveries, _ = st.ListDeliveries(ctx, run2.ID)
	if deliveries[0].Status != "failed" || deliveries[0].Attempts != len(retryDelays)+1 {
		t.Errorf("unreachable delivery = %+v", deliveries[0])
	}
}

func TestHousekeepingDeletesQueuedObjects(t *testing.T) {
	st := store.New(testdb.New(t, "callbacks_housekeeping_test"))
	ctx := context.Background()
	files := storage.NewMemory()
	key := "runs/x/obj"
	_ = files.Put(ctx, key, strings.NewReader("x"), 1, "text/plain")
	if _, err := st.Pool().Exec(ctx, `INSERT INTO storage_deletions (storage_key) VALUES ($1)`, key); err != nil {
		t.Fatal(err)
	}
	NewHousekeeping(st, files, slog.New(slog.NewTextHandler(io.Discard, nil))).Tick(ctx)
	if files.Len() != 0 {
		t.Error("object not deleted")
	}
	if left, _ := st.PendingStorageDeletions(ctx, 10); len(left) != 0 {
		t.Errorf("queue = %v", left)
	}
}
