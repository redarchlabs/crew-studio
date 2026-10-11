package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	azureAPIVersion = "2021-08-06"
	// Azurite's published development account (not a secret; it only works against the local emulator)
	azuriteAccount = "devstoreaccount1"
	azuriteKey     = "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw=="
	azuriteBlobURL = "http://127.0.0.1:10000/devstoreaccount1"
)

// Azure talks to Azure Blob Storage (or Azurite) with Shared Key authorization.
type Azure struct {
	account   string
	key       []byte
	endpoint  *url.URL
	container string
	emulator  bool
	client    *http.Client
}

// NewAzure parses an Azure Storage connection string (or "UseDevelopmentStorage=true" for Azurite).
func NewAzure(connectionString, container string) (*Azure, error) {
	if container == "" {
		return nil, errors.New("azure storage: container name is empty")
	}
	parts := map[string]string{}
	for _, field := range strings.Split(connectionString, ";") {
		if k, v, ok := strings.Cut(strings.TrimSpace(field), "="); ok {
			parts[strings.ToLower(k)] = v
		}
	}
	if strings.EqualFold(parts["usedevelopmentstorage"], "true") {
		parts["accountname"], parts["accountkey"], parts["blobendpoint"] = azuriteAccount, azuriteKey, azuriteBlobURL
	}
	account, keyB64 := parts["accountname"], parts["accountkey"]
	if account == "" || keyB64 == "" {
		return nil, errors.New("azure storage: the connection string needs AccountName and AccountKey")
	}
	key, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return nil, errors.New("azure storage: AccountKey is not valid base64")
	}
	endpoint := parts["blobendpoint"]
	if endpoint == "" {
		protocol := parts["defaultendpointsprotocol"]
		if protocol == "" {
			protocol = "https"
		}
		suffix := parts["endpointsuffix"]
		if suffix == "" {
			suffix = "core.windows.net"
		}
		endpoint = fmt.Sprintf("%s://%s.blob.%s", protocol, account, suffix)
	}
	u, err := url.Parse(strings.TrimRight(endpoint, "/"))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("azure storage: BlobEndpoint %q is not a URL", endpoint)
	}
	if u.Scheme != "https" && !isLoopbackHost(u.Hostname()) {
		return nil, errors.New("azure storage: use https for anything but a local emulator")
	}
	return &Azure{
		account: account, key: key, endpoint: u, container: container, client: newHTTPClient(),
		emulator: account == azuriteAccount,
	}, nil
}

func isLoopbackHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "azurite"
}

func (a *Azure) Describe() string {
	return fmt.Sprintf("azure blob %s/%s", a.endpoint.Host, a.container)
}

func (a *Azure) blobURL(key string) string {
	return a.endpoint.String() + "/" + a.container + "/" + key
}

// sign adds the Shared Key Authorization header (https://learn.microsoft.com/rest/api/storageservices/authorize-with-shared-key).
func (a *Azure) sign(req *http.Request, contentLength int64) {
	req.Header.Set("x-ms-date", time.Now().UTC().Format(http.TimeFormat))
	req.Header.Set("x-ms-version", azureAPIVersion)

	var msHeaders []string
	for name := range req.Header {
		if lower := strings.ToLower(name); strings.HasPrefix(lower, "x-ms-") {
			msHeaders = append(msHeaders, lower)
		}
	}
	sort.Strings(msHeaders)
	var canonHeaders strings.Builder
	for _, name := range msHeaders {
		canonHeaders.WriteString(name + ":" + strings.TrimSpace(req.Header.Get(name)) + "\n")
	}

	canonResource := "/" + a.account + req.URL.EscapedPath()
	query := req.URL.Query()
	names := make([]string, 0, len(query))
	for name := range query {
		names = append(names, strings.ToLower(name))
	}
	sort.Strings(names)
	for _, name := range names {
		values := query[name]
		sort.Strings(values)
		canonResource += "\n" + name + ":" + strings.Join(values, ",")
	}

	length := ""
	if contentLength > 0 {
		length = strconv.FormatInt(contentLength, 10)
	}
	toSign := strings.Join([]string{
		req.Method, req.Header.Get("Content-Encoding"), req.Header.Get("Content-Language"), length,
		req.Header.Get("Content-MD5"), req.Header.Get("Content-Type"), "", req.Header.Get("If-Modified-Since"),
		req.Header.Get("If-Match"), req.Header.Get("If-None-Match"), req.Header.Get("If-Unmodified-Since"),
		req.Header.Get("Range"),
	}, "\n") + "\n" + canonHeaders.String() + canonResource

	mac := hmac.New(sha256.New, a.key)
	mac.Write([]byte(toSign))
	req.Header.Set("Authorization", "SharedKey "+a.account+":"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
}

func (a *Azure) do(ctx context.Context, method, rawURL string, body io.Reader, size int64, contentType string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.ContentLength = size
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if method == http.MethodPut && !strings.Contains(rawURL, "restype=container") {
		req.Header.Set("x-ms-blob-type", "BlockBlob")
	}
	a.sign(req, size)
	return a.client.Do(req)
}

func (a *Azure) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if !ValidKey(key) {
		return fmt.Errorf("invalid object key %q", key)
	}
	if size == 0 {
		r = http.NoBody
	}
	res, err := a.do(ctx, http.MethodPut, a.blobURL(key), r, size, contentType)
	if err != nil {
		return fmt.Errorf("azure put %s: %w", key, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		return statusError("azure put "+key, res)
	}
	return nil
}

func (a *Azure) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if !ValidKey(key) {
		return nil, ErrNotFound
	}
	res, err := a.do(ctx, http.MethodGet, a.blobURL(key), nil, 0, "")
	if err != nil {
		return nil, fmt.Errorf("azure get %s: %w", key, err)
	}
	switch res.StatusCode {
	case http.StatusOK:
		return res.Body, nil
	case http.StatusNotFound:
		res.Body.Close()
		return nil, ErrNotFound
	default:
		defer res.Body.Close()
		return nil, statusError("azure get "+key, res)
	}
}

func (a *Azure) Delete(ctx context.Context, key string) error {
	if !ValidKey(key) {
		return nil
	}
	res, err := a.do(ctx, http.MethodDelete, a.blobURL(key), nil, 0, "")
	if err != nil {
		return fmt.Errorf("azure delete %s: %w", key, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusAccepted && res.StatusCode != http.StatusNotFound {
		return statusError("azure delete "+key, res)
	}
	return nil
}

// Ready creates the container if it is missing (it is private: no public access level is set).
func (a *Azure) Ready(ctx context.Context) error {
	res, err := a.do(ctx, http.MethodPut, a.endpoint.String()+"/"+a.container+"?restype=container", http.NoBody, 0, "")
	if err != nil {
		return fmt.Errorf("azure create container: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated && res.StatusCode != http.StatusConflict {
		return statusError("azure create container", res)
	}
	return nil
}
