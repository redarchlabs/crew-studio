package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	gcsDefaultEndpoint = "https://storage.googleapis.com"
	gcsScope           = "https://www.googleapis.com/auth/devstorage.read_write"
)

// GCS talks to Google Cloud Storage through its JSON API.
type GCS struct {
	bucket   string
	endpoint string
	client   *http.Client
}

// GCSConfig selects the bucket and credentials. With no CredentialsFile, Application Default Credentials
// are used (GOOGLE_APPLICATION_CREDENTIALS, gcloud, or the metadata server). An Endpoint other than the
// default (an emulator such as fake-gcs-server) is used without credentials.
type GCSConfig struct {
	Bucket          string
	CredentialsFile string
	Endpoint        string
}

func NewGCS(ctx context.Context, cfg GCSConfig) (*GCS, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("gcs storage: bucket name is empty")
	}
	endpoint := strings.TrimRight(cfg.Endpoint, "/")
	if endpoint == "" {
		endpoint = gcsDefaultEndpoint
	}
	g := &GCS{bucket: cfg.Bucket, endpoint: endpoint, client: newHTTPClient()}
	if endpoint != gcsDefaultEndpoint {
		return g, nil
	}

	var source oauth2.TokenSource
	if cfg.CredentialsFile != "" {
		data, err := os.ReadFile(cfg.CredentialsFile)
		if err != nil {
			return nil, fmt.Errorf("gcs storage: read credentials: %w", err)
		}
		creds, err := google.CredentialsFromJSON(ctx, data, gcsScope)
		if err != nil {
			return nil, fmt.Errorf("gcs storage: parse credentials: %w", err)
		}
		source = creds.TokenSource
	} else {
		creds, err := google.FindDefaultCredentials(ctx, gcsScope)
		if err != nil {
			return nil, fmt.Errorf("gcs storage: no credentials (set STUDIO_GCS_CREDENTIALS_FILE or GOOGLE_APPLICATION_CREDENTIALS): %w", err)
		}
		source = creds.TokenSource
	}
	authed := &http.Client{
		Transport:     &oauth2.Transport{Source: source, Base: g.client.Transport},
		CheckRedirect: g.client.CheckRedirect,
	}
	g.client = authed
	return g, nil
}

func (g *GCS) Describe() string { return "gcs " + g.bucket }

func (g *GCS) objectURL(key string) string {
	return fmt.Sprintf("%s/storage/v1/b/%s/o/%s", g.endpoint, url.PathEscape(g.bucket), url.PathEscape(key))
}

func (g *GCS) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if !ValidKey(key) {
		return fmt.Errorf("invalid object key %q", key)
	}
	u := fmt.Sprintf("%s/upload/storage/v1/b/%s/o?uploadType=media&name=%s", g.endpoint, url.PathEscape(g.bucket), url.QueryEscape(key))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, r)
	if err != nil {
		return err
	}
	req.ContentLength = size
	if size == 0 {
		req.Body = http.NoBody
	}
	req.Header.Set("Content-Type", contentType)
	res, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("gcs put %s: %w", key, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return statusError("gcs put "+key, res)
	}
	return nil
}

func (g *GCS) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if !ValidKey(key) {
		return nil, ErrNotFound
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.objectURL(key)+"?alt=media", nil)
	if err != nil {
		return nil, err
	}
	res, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gcs get %s: %w", key, err)
	}
	switch res.StatusCode {
	case http.StatusOK:
		return res.Body, nil
	case http.StatusNotFound:
		res.Body.Close()
		return nil, ErrNotFound
	default:
		defer res.Body.Close()
		return nil, statusError("gcs get "+key, res)
	}
}

func (g *GCS) Delete(ctx context.Context, key string) error {
	if !ValidKey(key) {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, g.objectURL(key), nil)
	if err != nil {
		return err
	}
	res, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("gcs delete %s: %w", key, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent && res.StatusCode != http.StatusOK && res.StatusCode != http.StatusNotFound {
		return statusError("gcs delete "+key, res)
	}
	return nil
}

// Ready checks the bucket exists. Buckets are not created automatically: location, retention and access
// policy are decisions for whoever owns the Google Cloud project.
func (g *GCS) Ready(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/storage/v1/b/%s", g.endpoint, url.PathEscape(g.bucket)), nil)
	if err != nil {
		return err
	}
	res, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("gcs check bucket: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return statusError("gcs check bucket "+g.bucket, res)
	}
	return nil
}
