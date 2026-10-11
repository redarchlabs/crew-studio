package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func studio(t *testing.T, env string) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), env)
	writeFile(t, filepath.Join(dir, "runner/run_crew.py"), "")
	writeFile(t, filepath.Join(dir, "runner/tools_catalog.json"), "[]")
	writeFile(t, filepath.Join(dir, "py"), "")
	return dir
}

func noEnv(string) string { return "" }

func TestReadDotEnv(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".env"), "# comment\nA=1\nexport B=\"two words\"\nC='x'\nbroken line\n\nD=a=b\n")
	got, err := ReadDotEnv(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"A": "1", "B": "two words", "C": "x", "D": "a=b"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %v", got)
	}
}

func TestLoadResolvesRelativePaths(t *testing.T) {
	dir := studio(t, "STUDIO_DATABASE_URL=postgres://x\nSTUDIO_PYTHON=py\nSTUDIO_ALLOWED_ORIGINS=http://a, http://b\n")
	cfg, err := Load(dir, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Python != filepath.Join(dir, "py") || cfg.CatalogPath != filepath.Join(dir, "runner/tools_catalog.json") {
		t.Errorf("paths = %q %q", cfg.Python, cfg.CatalogPath)
	}
	if len(cfg.AllowedOrigins) != 2 || cfg.AllowedOrigins[1] != "http://b" {
		t.Errorf("origins = %v", cfg.AllowedOrigins)
	}
	if cfg.Addr != defaultAddr || cfg.MaxRunDuration != 240*time.Minute {
		t.Errorf("defaults = %q %v", cfg.Addr, cfg.MaxRunDuration)
	}
}

func TestEnvironmentWinsOverFile(t *testing.T) {
	dir := studio(t, "STUDIO_DATABASE_URL=postgres://file\nSTUDIO_PYTHON=py\n")
	cfg, err := Load(dir, func(k string) string {
		if k == "STUDIO_DATABASE_URL" {
			return "postgres://env"
		}
		return ""
	})
	if err != nil || cfg.DatabaseURL != "postgres://env" {
		t.Fatalf("cfg = %+v, %v", cfg, err)
	}
}

func TestLoadRejectsUnsafeOrMissingSettings(t *testing.T) {
	tests := []struct {
		name, env, want string
	}{
		{"missing db", "STUDIO_PYTHON=py\n", "STUDIO_DATABASE_URL"},
		{"public bind", "STUDIO_DATABASE_URL=x\nSTUDIO_PYTHON=py\nSTUDIO_ADDR=0.0.0.0:8088\n", "loopback"},
		{"missing python", "STUDIO_DATABASE_URL=x\nSTUDIO_PYTHON=nope\n", "STUDIO_PYTHON not found"},
		{"bad minutes", "STUDIO_DATABASE_URL=x\nSTUDIO_PYTHON=py\nSTUDIO_MAX_RUN_MINUTES=0\n", "minutes"},
		{"workspace exposes secrets", "STUDIO_DATABASE_URL=x\nSTUDIO_PYTHON=py\nSTUDIO_WORKSPACE=.\n", "agents could read it"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(studio(t, tt.env), noEnv)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want mention of %q", err, tt.want)
			}
		})
	}
}

func TestAllowRemote(t *testing.T) {
	base := "STUDIO_DATABASE_URL=x\nSTUDIO_PYTHON=py\nSTUDIO_ADDR=0.0.0.0:8088\nSTUDIO_ALLOW_REMOTE=true\n"
	if _, err := Load(studio(t, base+"STUDIO_PUBLIC_URL=https://studio.example.com\n"), noEnv); err != nil {
		t.Fatalf("remote bind should be allowed when opted in: %v", err)
	}
	if _, err := Load(studio(t, base+"STUDIO_PUBLIC_URL=http://studio.example.com\n"), noEnv); err == nil {
		t.Error("remote access over plain http must be refused")
	}
}

func TestSSOAndStorageSettings(t *testing.T) {
	base := "STUDIO_DATABASE_URL=x\nSTUDIO_PYTHON=py\n"
	cfg, err := Load(studio(t, base+"STUDIO_MICROSOFT_TENANT_ID=t\nSTUDIO_MICROSOFT_CLIENT_ID=c\nSTUDIO_MICROSOFT_CLIENT_SECRET=s\n"), noEnv)
	if err != nil || cfg.Microsoft == nil || cfg.Clerk != nil || cfg.Microsoft.Issuer != "t" {
		t.Fatalf("microsoft = %+v, err %v", cfg.Microsoft, err)
	}
	if cfg.Storage.Driver != StorageAzure || cfg.Storage.AzureConnection != "UseDevelopmentStorage=true" || !cfg.LocalLogin {
		t.Errorf("defaults = %+v local=%v", cfg.Storage, cfg.LocalLogin)
	}
	if cfg.PublicURL != "http://127.0.0.1:8088" {
		t.Errorf("public url = %q", cfg.PublicURL)
	}
	if cfg.HelpSearch != HelpSearchOpenAI {
		t.Errorf("help search defaults to %q", cfg.HelpSearch)
	}
	for name, env := range map[string]string{
		"bad help search": "STUDIO_HELP_SEARCH=semantic\n",
		"half sso":        "STUDIO_CLERK_ISSUER=https://x.clerk.accounts.dev\n",
		"bad storage":     "STUDIO_STORAGE=s3\n",
		"gcs bucket":      "STUDIO_STORAGE=gcs\n",
		"data in ws":      "STUDIO_DATA_DIR=workspace/data\n",
		"public url":      "STUDIO_PUBLIC_URL=studio.example.com\n",
		"public url path": "STUDIO_PUBLIC_URL=https://example.com/studio\n",
	} {
		if _, err := Load(studio(t, base+env), noEnv); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	cfg, err = Load(studio(t, base+"STUDIO_LOCAL_LOGIN=false\nSTUDIO_STORAGE=none\nSTUDIO_HELP_SEARCH=Gemini\n"), noEnv)
	if err != nil || cfg.LocalLogin || cfg.Storage.Driver != StorageNone || cfg.HelpSearch != HelpSearchGemini {
		t.Errorf("cfg = %+v err %v", cfg, err)
	}
}

func TestMemoryEmbeddingsSetting(t *testing.T) {
	base := "STUDIO_DATABASE_URL=x\nSTUDIO_PYTHON=py\n"
	for env, want := range map[string]string{
		"":                             HelpSearchOpenAI,
		"STUDIO_HELP_SEARCH=gemini\n":  HelpSearchGemini,
		"STUDIO_HELP_SEARCH=keyword\n": MemoryEmbeddingsOff,
		"STUDIO_HELP_SEARCH=keyword\nSTUDIO_MEMORY_EMBEDDINGS=OpenAI\n": HelpSearchOpenAI,
		"STUDIO_MEMORY_EMBEDDINGS=off\n":                                MemoryEmbeddingsOff,
	} {
		cfg, err := Load(studio(t, base+env), noEnv)
		if err != nil || cfg.MemoryEmbeddings != want {
			t.Errorf("%q: memory embeddings = %q, err %v; want %q", env, cfg.MemoryEmbeddings, err, want)
		}
	}
	if _, err := Load(studio(t, base+"STUDIO_MEMORY_EMBEDDINGS=keyword\n"), noEnv); err == nil {
		t.Error("keyword is not a memory embedder")
	}
}
