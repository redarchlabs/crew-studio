package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crewstudio/internal/config"
	"crewstudio/internal/storage"
	"crewstudio/internal/store"
	"crewstudio/internal/testdb"
)

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestDefaultStudioDir(t *testing.T) {
	root := t.TempDir()
	server := filepath.Join(root, "server")
	if err := os.Mkdir(server, 0o755); err != nil {
		t.Fatal(err)
	}
	for dir, want := range map[string]string{server: root, root: root} {
		t.Chdir(dir)
		got, err := filepath.EvalSymlinks(defaultStudioDir())
		if err != nil {
			t.Fatal(err)
		}
		wantReal, _ := filepath.EvalSymlinks(want)
		if got != wantReal {
			t.Errorf("from %s: got %s, want %s", dir, got, wantReal)
		}
	}
}

func TestNewLoggerLevels(t *testing.T) {
	ctx := context.Background()
	for env, want := range map[string]slog.Level{
		"": slog.LevelInfo, "debug": slog.LevelDebug, "DEBUG": slog.LevelDebug,
		"warn": slog.LevelWarn, "error": slog.LevelError, "bogus": slog.LevelInfo,
	} {
		t.Setenv("STUDIO_LOG_LEVEL", env)
		log := newLogger()
		if !log.Enabled(ctx, want) || (want > slog.LevelDebug && log.Enabled(ctx, want-1)) {
			t.Errorf("STUDIO_LOG_LEVEL=%q: lowest enabled level is not %v", env, want)
		}
	}
}

func TestOpenStorageNoneIsOff(t *testing.T) {
	files, err := openStorage(context.Background(), config.StorageConfig{Driver: config.StorageNone}, quietLog())
	if err != nil || files != nil {
		t.Fatalf("got %v, %v; want nil, nil", files, err)
	}
	if describeStorage(nil) != "off" {
		t.Error(`describeStorage(nil) should be "off"`)
	}
	if describeStorage(storage.NewMemory()) != "memory" {
		t.Error("describeStorage should use the store's description")
	}
}

func TestOpenStorageConfigErrors(t *testing.T) {
	ctx := context.Background()
	for name, c := range map[string]config.StorageConfig{
		"azure without container": {Driver: config.StorageAzure, AzureConnection: "UseDevelopmentStorage=true"},
		"azure bad connection":    {Driver: config.StorageAzure, AzureConnection: "nonsense", AzureContainer: "c"},
		"gcs missing credentials": {Driver: config.StorageGCS, GCSBucket: "b", GCSCredentials: filepath.Join(t.TempDir(), "missing.json")},
	} {
		t.Run(name, func(t *testing.T) {
			if files, err := openStorage(ctx, c, quietLog()); err == nil || files != nil {
				t.Fatalf("got %v, %v; want an error", files, err)
			}
		})
	}
}

func TestOpenStorageUnreachable(t *testing.T) {
	// A port nothing listens on: the readiness check must fail with a clear message instead of starting up
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	conn := "DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=a2V5;BlobEndpoint=http://" + addr + "/devstoreaccount1"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err = openStorage(ctx, config.StorageConfig{Driver: config.StorageAzure, AzureConnection: conn, AzureContainer: "c"}, quietLog())
	if err == nil || !strings.Contains(err.Error(), "is not reachable") {
		t.Fatalf("err = %v, want a not-reachable error", err)
	}
}

func TestOpenStorageAzurite(t *testing.T) {
	c, err := net.DialTimeout("tcp", "127.0.0.1:10000", time.Second)
	if err != nil {
		t.Skip("Azurite is not running on 127.0.0.1:10000")
	}
	_ = c.Close()
	files, err := openStorage(context.Background(), config.StorageConfig{
		Driver: config.StorageAzure, AzureConnection: "UseDevelopmentStorage=true", AzureContainer: "crewstudio-maintest",
	}, quietLog())
	if err != nil || files == nil {
		t.Fatalf("got %v, %v", files, err)
	}
	if !strings.Contains(describeStorage(files), "crewstudio-maintest") {
		t.Errorf("description %q should name the container", describeStorage(files))
	}
}

func TestNewSSO(t *testing.T) {
	if got := newSSO(config.Config{PublicURL: "http://localhost:8088"}).Providers(); len(got) != 0 {
		t.Errorf("no providers configured, got %v", got)
	}
	sso := newSSO(config.Config{
		PublicURL: "https://studio.example.com",
		Microsoft: &config.SSOProvider{Issuer: "tenant-id", ClientID: "ms", ClientSecret: "s"},
		Clerk:     &config.SSOProvider{Issuer: "https://clerk.example.com", ClientID: "ck", ClientSecret: "s"},
	})
	if !sso.Has("microsoft") || !sso.Has("clerk") {
		t.Fatalf("providers = %v", sso.Providers())
	}
	if got := sso.RedirectURL("clerk"); got != "https://studio.example.com/api/auth/clerk/callback" {
		t.Errorf("redirect URL = %s", got)
	}
}

func TestFirstRunToken(t *testing.T) {
	st := store.New(testdb.New(t, "main_unit"))
	ctx := context.Background()
	cfg := config.Config{PublicURL: "http://localhost:8088"}

	token, err := firstRunToken(ctx, st, cfg, quietLog())
	if err != nil || len(token) < 24 {
		t.Fatalf("with no accounts: token %q, err %v", token, err)
	}
	if again, _ := firstRunToken(ctx, st, cfg, quietLog()); again == token {
		t.Error("each start should mint a new token")
	}

	if _, err := st.CreateFirstAdmin(ctx, "ada@example.com", "Ada", "not-a-real-bcrypt-hash"); err != nil {
		t.Fatal(err)
	}
	if token, err := firstRunToken(ctx, st, cfg, quietLog()); err != nil || token != "" {
		t.Errorf("once an account exists: token %q, err %v; want none", token, err)
	}
}
