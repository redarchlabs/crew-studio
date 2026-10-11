// Package config loads Crew Studio settings from the environment and studio/.env.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL     string
	Addr            string
	Python          string
	RunnerScript    string
	CatalogPath     string
	ProviderEnvFile string
	Workspace       string
	DataDir         string
	KeepRunFiles    bool
	WebDir          string
	AllowedOrigins  []string
	AllowedHosts    []string
	MaxRunDuration  time.Duration

	// PublicURL is the address people use; SSO redirect URLs are built from it
	PublicURL  string
	LocalLogin bool
	Clerk      *SSOProvider
	Microsoft  *SSOProvider
	Storage    StorageConfig
	// HelpSearch picks the embeddings behind help search: "openai", "gemini", or "keyword" for none
	HelpSearch string
	// MemoryEmbeddings picks the embeddings behind crew memory: "openai", "gemini", or "off"; by default the same
	// provider as help search (off when help search is keyword)
	MemoryEmbeddings string
	// SecretsKey encrypts connection secrets (STUDIO_SECRETS_KEY); empty turns connections off
	SecretsKey string
	// JiraSites are Jira site origins allowed besides https://*.atlassian.net (STUDIO_JIRA_SITES, comma-separated)
	JiraSites []string
	// KM2TrackerSites, when set, are the only KM2 origins km2_tracker connections may use (STUDIO_KM2_TRACKER_SITES,
	// comma-separated); unset allows any https address
	KM2TrackerSites []string
	// LinearAPIURL replaces Linear's GraphQL endpoint (STUDIO_LINEAR_API_URL; tests and proxies only)
	LinearAPIURL string
	// DeliveryOrigins are extra origins report webhooks may post to (STUDIO_DELIVERY_ORIGINS, comma-separated;
	// plain http only for loopback, for tests)
	DeliveryOrigins []string
	// SlackAPIURL replaces https://slack.com/api (STUDIO_SLACK_API_URL; tests only)
	SlackAPIURL string
}

// SSOProvider holds one OpenID Connect client registration.
type SSOProvider struct {
	// Issuer is Clerk's Frontend API URL; for Microsoft it is the tenant id
	Issuer       string
	ClientID     string
	ClientSecret string
}

const (
	HelpSearchOpenAI    = "openai"
	HelpSearchGemini    = "gemini"
	HelpSearchKeyword   = "keyword"
	MemoryEmbeddingsOff = "off"
)

const (
	StorageAzure = "azure"
	StorageGCS   = "gcs"
	StorageNone  = "none"
)

// StorageConfig selects where attachments, artifacts and transcripts are kept.
type StorageConfig struct {
	Driver          string
	AzureConnection string
	AzureContainer  string
	GCSBucket       string
	GCSCredentials  string
	GCSEndpoint     string
}

const (
	defaultAddr          = "127.0.0.1:8088"
	defaultMaxRunMinutes = 240
	allowRemoteEnvVar    = "STUDIO_ALLOW_REMOTE"
	maxRunMinutesEnvVar  = "STUDIO_MAX_RUN_MINUTES"
	catalogFileName      = "tools_catalog.json"
)

// ReadDotEnv parses KEY=VALUE lines (comments, blank lines, "export " and quotes allowed).
func ReadDotEnv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		}
		values[strings.TrimSpace(key)] = value
	}
	return values, scanner.Err()
}

// Load builds the config. studioDir is the folder holding .env; relative paths resolve against it.
// Real environment variables win over values in studio/.env.
func Load(studioDir string, getenv func(string) string) (Config, error) {
	fileValues, err := ReadDotEnv(filepath.Join(studioDir, ".env"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("read studio .env: %w", err)
	}
	get := func(key, fallback string) string {
		if v := getenv(key); v != "" {
			return v
		}
		if v := fileValues[key]; v != "" {
			return v
		}
		return fallback
	}
	resolve := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(studioDir, p)
	}

	cfg := Config{
		DatabaseURL:     get("STUDIO_DATABASE_URL", ""),
		Addr:            get("STUDIO_ADDR", defaultAddr),
		Python:          resolve(get("STUDIO_PYTHON", "../.venv/bin/python")),
		RunnerScript:    resolve(get("STUDIO_RUNNER", "runner/run_crew.py")),
		ProviderEnvFile: resolve(get("STUDIO_PROVIDER_ENV_FILE", "../.env")),
		Workspace:       resolve(get("STUDIO_WORKSPACE", "workspace")),
		WebDir:          resolve(get("STUDIO_WEB_DIR", "")),
	}
	cfg.CatalogPath = filepath.Join(filepath.Dir(cfg.RunnerScript), catalogFileName)
	cfg.DataDir = resolve(get("STUDIO_DATA_DIR", "data"))
	cfg.KeepRunFiles = get("STUDIO_KEEP_RUN_FILES", "") == "true"
	cfg.PublicURL = strings.TrimRight(get("STUDIO_PUBLIC_URL", "http://"+cfg.Addr), "/")
	cfg.LocalLogin = get("STUDIO_LOCAL_LOGIN", "true") != "false"
	var ssoProblems []string
	cfg.Clerk, ssoProblems = ssoProvider(get, "STUDIO_CLERK_ISSUER", "STUDIO_CLERK_CLIENT_ID", "STUDIO_CLERK_CLIENT_SECRET")
	var msProblems []string
	cfg.Microsoft, msProblems = ssoProvider(get, "STUDIO_MICROSOFT_TENANT_ID", "STUDIO_MICROSOFT_CLIENT_ID", "STUDIO_MICROSOFT_CLIENT_SECRET")
	ssoProblems = append(ssoProblems, msProblems...)
	cfg.Storage = StorageConfig{
		Driver:          strings.ToLower(get("STUDIO_STORAGE", StorageAzure)),
		AzureConnection: get("STUDIO_AZURE_STORAGE_CONNECTION_STRING", "UseDevelopmentStorage=true"),
		AzureContainer:  get("STUDIO_AZURE_CONTAINER", "crewstudio"),
		GCSBucket:       get("STUDIO_GCS_BUCKET", ""),
		GCSCredentials:  resolve(get("STUDIO_GCS_CREDENTIALS_FILE", "")),
		GCSEndpoint:     get("STUDIO_GCS_ENDPOINT", ""),
	}
	cfg.HelpSearch = strings.ToLower(get("STUDIO_HELP_SEARCH", HelpSearchOpenAI))
	memoryDefault := cfg.HelpSearch
	if memoryDefault == HelpSearchKeyword {
		memoryDefault = MemoryEmbeddingsOff
	}
	cfg.MemoryEmbeddings = strings.ToLower(strings.TrimSpace(get("STUDIO_MEMORY_EMBEDDINGS", memoryDefault)))
	cfg.SecretsKey = get("STUDIO_SECRETS_KEY", "")
	cfg.LinearAPIURL = strings.TrimSpace(get("STUDIO_LINEAR_API_URL", ""))
	var trackerProblems []string
	cfg.JiraSites, trackerProblems = trackerSites(get("STUDIO_JIRA_SITES", ""), cfg.LinearAPIURL)
	ssoProblems = append(ssoProblems, trackerProblems...)
	cfg.KM2TrackerSites, trackerProblems = originList("STUDIO_KM2_TRACKER_SITES", get("STUDIO_KM2_TRACKER_SITES", ""), "https://km2.example.com")
	ssoProblems = append(ssoProblems, trackerProblems...)
	var deliveryProblems []string
	cfg.SlackAPIURL = strings.TrimSpace(get("STUDIO_SLACK_API_URL", ""))
	cfg.DeliveryOrigins, deliveryProblems = deliverySettings(get("STUDIO_DELIVERY_ORIGINS", ""), cfg.SlackAPIURL)
	ssoProblems = append(ssoProblems, deliveryProblems...)
	for _, origin := range strings.Split(get("STUDIO_ALLOWED_ORIGINS", ""), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			cfg.AllowedOrigins = append(cfg.AllowedOrigins, origin)
		}
	}
	for _, host := range strings.Split(get("STUDIO_ALLOWED_HOSTS", ""), ",") {
		if host = strings.ToLower(strings.TrimSpace(host)); host != "" {
			cfg.AllowedHosts = append(cfg.AllowedHosts, host)
		}
	}
	minutes, err := strconv.Atoi(get(maxRunMinutesEnvVar, strconv.Itoa(defaultMaxRunMinutes)))
	if err != nil || minutes < 1 {
		return Config{}, fmt.Errorf("%s must be a whole number of minutes", maxRunMinutesEnvVar)
	}
	cfg.MaxRunDuration = time.Duration(minutes) * time.Minute

	if err := cfg.validate(get(allowRemoteEnvVar, "") == "true", filepath.Join(studioDir, ".env"), ssoProblems); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// ssoProvider reads one provider: all three settings, or none (provider off).
func ssoProvider(get func(string, string) string, issuerVar, idVar, secretVar string) (*SSOProvider, []string) {
	p := &SSOProvider{Issuer: get(issuerVar, ""), ClientID: get(idVar, ""), ClientSecret: get(secretVar, "")}
	set := 0
	var missing []string
	for name, v := range map[string]string{issuerVar: p.Issuer, idVar: p.ClientID, secretVar: p.ClientSecret} {
		if v != "" {
			set++
		} else {
			missing = append(missing, name)
		}
	}
	switch set {
	case 0:
		return nil, nil
	case 3:
		return p, nil
	}
	slices.Sort(missing)
	return nil, []string{"single sign-on is half configured; also set " + strings.Join(missing, ", ")}
}

// within reports whether path is dir itself or inside it.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (c Config) validate(allowRemote bool, studioEnvFile string, problems []string) error {
	// Agents can read anything in the workspace, so it must not contain a file holding secrets
	for _, secret := range []string{c.ProviderEnvFile, studioEnvFile} {
		if absWorkspace, err := filepath.Abs(c.Workspace); err == nil && secret != "" {
			if absSecret, err := filepath.Abs(secret); err == nil && within(absWorkspace, absSecret) {
				problems = append(problems, fmt.Sprintf("STUDIO_WORKSPACE %s contains %s; agents could read it", c.Workspace, secret))
			}
		}
	}
	if absWorkspace, err := filepath.Abs(c.Workspace); err == nil {
		if absData, err := filepath.Abs(c.DataDir); err == nil && (within(absWorkspace, absData) || within(absData, absWorkspace)) {
			problems = append(problems, "STUDIO_DATA_DIR and STUDIO_WORKSPACE must be separate folders (agents must not reach run transcripts)")
		}
	}
	problems = append(problems, c.checkPublicURL(allowRemote)...)
	switch c.Storage.Driver {
	case StorageAzure:
		if c.Storage.AzureConnection == "" || c.Storage.AzureContainer == "" {
			problems = append(problems, "STUDIO_STORAGE=azure needs STUDIO_AZURE_STORAGE_CONNECTION_STRING and STUDIO_AZURE_CONTAINER")
		}
	case StorageGCS:
		if c.Storage.GCSBucket == "" {
			problems = append(problems, "STUDIO_STORAGE=gcs needs STUDIO_GCS_BUCKET")
		}
	case StorageNone:
	default:
		problems = append(problems, fmt.Sprintf("STUDIO_STORAGE %q must be azure, gcs or none", c.Storage.Driver))
	}
	if !slices.Contains([]string{HelpSearchOpenAI, HelpSearchGemini, HelpSearchKeyword}, c.HelpSearch) {
		problems = append(problems, fmt.Sprintf("STUDIO_HELP_SEARCH %q must be openai, gemini or keyword", c.HelpSearch))
	}
	if !slices.Contains([]string{HelpSearchOpenAI, HelpSearchGemini, MemoryEmbeddingsOff}, c.MemoryEmbeddings) {
		problems = append(problems, fmt.Sprintf("STUDIO_MEMORY_EMBEDDINGS %q must be openai, gemini or off", c.MemoryEmbeddings))
	}
	if c.DatabaseURL == "" {
		problems = append(problems, "STUDIO_DATABASE_URL is not set")
	}
	host, _, err := net.SplitHostPort(c.Addr)
	if err != nil {
		problems = append(problems, fmt.Sprintf("STUDIO_ADDR %q is not host:port", c.Addr))
	} else if ip := net.ParseIP(host); !allowRemote && host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		// Listening beyond this machine is a deliberate choice; it also needs https (see checkPublicURL)
		problems = append(problems, fmt.Sprintf("STUDIO_ADDR %q is not a loopback address; set %s=true to allow it", c.Addr, allowRemoteEnvVar))
	}
	for _, path := range []struct{ name, value string }{
		{"STUDIO_PYTHON", c.Python}, {"STUDIO_RUNNER", c.RunnerScript}, {"tool catalog", c.CatalogPath},
	} {
		if _, err := os.Stat(path.value); err != nil {
			problems = append(problems, fmt.Sprintf("%s not found at %s", path.name, path.value))
		}
	}
	if len(problems) > 0 {
		return errors.New("invalid configuration: " + strings.Join(problems, "; "))
	}
	return nil
}

// checkPublicURL: SSO redirects and cookies depend on it, and remote access must use https.
func (c Config) checkPublicURL(allowRemote bool) []string {
	u, err := url.Parse(c.PublicURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" {
		return []string{fmt.Sprintf("STUDIO_PUBLIC_URL %q must look like https://studio.example.com (no path)", c.PublicURL)}
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	local := host == "localhost" || (ip != nil && ip.IsLoopback())
	if allowRemote && !local && u.Scheme != "https" {
		return []string{"STUDIO_PUBLIC_URL must use https when Crew Studio is reachable from other machines"}
	}
	return nil
}

// InternalURL is how processes on this machine (the runner's state tools) reach the API: the listen address,
// with a wildcard host replaced by loopback.
func (c Config) InternalURL() string {
	host, port, err := net.SplitHostPort(c.Addr)
	if err != nil {
		return "http://" + c.Addr
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// trackerSites reads STUDIO_JIRA_SITES (origins such as https://jira.example.com) and checks STUDIO_LINEAR_API_URL.
func trackerSites(list, linearURL string) ([]string, []string) {
	sites, problems := originList("STUDIO_JIRA_SITES", list, "https://jira.example.com")
	if linearURL != "" {
		if u, err := url.Parse(linearURL); err != nil || !secureOrLoopback(u) || u.Host == "" || u.User != nil {
			problems = append(problems, "STUDIO_LINEAR_API_URL must be an https URL (http only for loopback)")
		}
	}
	return sites, problems
}

// originList reads a comma-separated list of origins (https, or http only for loopback; no path, user or query).
func originList(name, list, example string) ([]string, []string) {
	var sites, problems []string
	for _, origin := range strings.Split(list, ",") {
		origin = strings.TrimRight(strings.TrimSpace(origin), "/")
		if origin == "" {
			continue
		}
		u, err := url.Parse(origin)
		if err != nil || !secureOrLoopback(u) || u.Host == "" || u.Path != "" || u.User != nil || u.RawQuery != "" {
			problems = append(problems, fmt.Sprintf("%s: %q must be an https origin such as %s (http only for loopback)", name, origin, example))
			continue
		}
		sites = append(sites, u.Scheme+"://"+u.Host)
	}
	return sites, problems
}

// secureOrLoopback allows https anywhere and http only to this machine, so a tracker token never crosses a network
// in clear text.
func secureOrLoopback(u *url.URL) bool {
	if u.Scheme == "https" {
		return true
	}
	if u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
