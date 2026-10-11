// Package callbacks POSTs signed run results to the callback URLs that triggers and service accounts ask for.
package callbacks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"crewstudio/internal/model"
	"crewstudio/internal/store"
)

const (
	pollInterval   = 10 * time.Second
	claimLease     = 2 * time.Minute
	batchSize      = 10
	requestTimeout = 10 * time.Second
	maxOutputChars = 100_000
	userAgent      = "CrewStudio-Callbacks/1"
)

// retryDelays are the waits after each failed attempt; after the last one the delivery is marked failed.
var retryDelays = []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute, 2 * time.Hour}

type Deliverer struct {
	store *store.Store
	log   *slog.Logger
	now   func() time.Time
	// dial lets tests reach a local server; nil uses the guarded dialer
	dial func(allowPrivate bool) func(ctx context.Context, network, addr string) (net.Conn, error)
}

func New(st *store.Store, log *slog.Logger) *Deliverer {
	return &Deliverer{store: st, log: log, now: time.Now}
}

func (d *Deliverer) Run(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		d.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Tick sends every delivery that is due.
func (d *Deliverer) Tick(ctx context.Context) {
	due, err := d.store.ClaimDueDeliveries(ctx, batchSize, claimLease)
	if err != nil {
		if ctx.Err() == nil {
			d.log.Error("claim callback deliveries", "err", err)
		}
		return
	}
	for _, del := range due {
		d.deliver(ctx, del)
	}
}

func (d *Deliverer) deliver(ctx context.Context, del store.Delivery) {
	code, err := d.send(ctx, del)
	var codePtr *int
	if code != 0 {
		codePtr = &code
	}
	if err == nil {
		if rerr := d.store.RecordDeliveryAttempt(ctx, del.ID, true, codePtr, nil, nil); rerr != nil {
			d.log.Error("record callback delivery", "delivery_id", del.ID, "err", rerr)
		}
		return
	}
	msg := err.Error()
	var next *time.Time
	if del.Attempts < len(retryDelays) {
		t := d.now().Add(retryDelays[del.Attempts])
		next = &t
	}
	d.log.Warn("callback delivery failed", "delivery_id", del.ID, "run_id", del.RunID, "attempt", del.Attempts+1, "err", err)
	if rerr := d.store.RecordDeliveryAttempt(ctx, del.ID, false, codePtr, &msg, next); rerr != nil {
		d.log.Error("record callback delivery", "delivery_id", del.ID, "err", rerr)
	}
}

type payload struct {
	Event       string        `json:"event"`
	DeliveredAt time.Time     `json:"delivered_at"`
	Run         runSummary    `json:"run"`
	WorkOrder   *workOrderRef `json:"work_order"`
}

type runSummary struct {
	ID          string            `json:"id"`
	Status      model.RunStatus   `json:"status"`
	CrewName    string            `json:"crew_name"`
	Output      *string           `json:"output"`
	Error       *string           `json:"error"`
	CostUSD     *float64          `json:"cost_usd"`
	Usage       *model.TokenUsage `json:"usage"`
	StartedAt   *time.Time        `json:"started_at"`
	FinishedAt  *time.Time        `json:"finished_at"`
	TriggerType string            `json:"trigger_type"`
}

type workOrderRef struct {
	ID     string `json:"id"`
	Number int64  `json:"number"`
	Title  string `json:"title"`
}

func clip(s *string) *string {
	if s == nil || len(*s) <= maxOutputChars {
		return s
	}
	cut := (*s)[:maxOutputChars] + "\n… [cut; read the full output from the API]"
	return &cut
}

func (d *Deliverer) body(ctx context.Context, del store.Delivery) ([]byte, error) {
	run, err := d.store.GetRun(ctx, del.RunID)
	if err != nil {
		return nil, fmt.Errorf("load run: %w", err)
	}
	p := payload{
		Event: "run.finished", DeliveredAt: d.now().UTC(),
		Run: runSummary{
			ID: run.ID, Status: run.Status, CrewName: run.CrewName, Output: clip(run.Output), Error: run.Error,
			CostUSD: run.CostUSD, Usage: run.Usage, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, TriggerType: run.TriggerType,
		},
	}
	if run.WorkOrderID != nil {
		if wo, err := d.store.GetWorkOrder(ctx, *run.WorkOrderID); err == nil {
			p.WorkOrder = &workOrderRef{ID: wo.ID, Number: wo.Number, Title: wo.Title}
		}
	}
	return json.Marshal(p)
}

func (d *Deliverer) secret(ctx context.Context, del store.Delivery) (string, error) {
	switch del.SecretSource {
	case store.CallbackFromTrigger:
		return d.store.TriggerSigningSecret(ctx, del.SecretOwnerID)
	case store.CallbackFromServiceAccount:
		return d.store.SigningSecret(ctx, del.SecretOwnerID)
	}
	return "", fmt.Errorf("unknown secret source %q", del.SecretSource)
}

// Sign returns the X-CrewStudio-Signature value for a body sent at timestamp.
func Sign(secret string, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(timestamp, 10) + "."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func (d *Deliverer) send(ctx context.Context, del store.Delivery) (int, error) {
	target, err := url.Parse(del.URL)
	if err != nil || target.Host == "" {
		return 0, errors.New("the callback URL is not valid")
	}
	settings, err := d.store.GetAccessSettings(ctx)
	if err != nil {
		return 0, err
	}
	allowPrivate := slices.Contains(settings.CallbackAllowedHosts, strings.ToLower(target.Hostname()))
	if target.Scheme != "https" && !allowPrivate {
		return 0, errors.New("callback URLs must use https unless the host is on the allowed list")
	}
	secret, err := d.secret(ctx, del)
	if err != nil {
		return 0, fmt.Errorf("signing secret: %w", err)
	}
	body, err := d.body(ctx, del)
	if err != nil {
		return 0, err
	}
	ts := d.now().Unix()
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-CrewStudio-Event", "run.finished")
	req.Header.Set("X-CrewStudio-Delivery", del.ID)
	req.Header.Set("X-CrewStudio-Timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("X-CrewStudio-Signature", Sign(secret, ts, body))

	dial := guardedDial
	if d.dial != nil {
		dial = d.dial
	}
	client := &http.Client{
		Transport: &http.Transport{DialContext: dial(allowPrivate), Proxy: nil, TLSHandshakeTimeout: requestTimeout,
			ResponseHeaderTimeout: requestTimeout, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	res, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("send: %w", err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return res.StatusCode, fmt.Errorf("the receiver answered HTTP %d", res.StatusCode)
	}
	return res.StatusCode, nil
}

// guardedDial connects only to public addresses (checked after DNS resolution, at connect time, so a DNS
// answer cannot point the request at an internal service), unless the host is allow-listed.
func guardedDial(allowPrivate bool) func(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: requestTimeout, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return fmt.Errorf("refusing to connect to %s", host)
		}
		// Even allow-listed internal hosts never reach link-local (cloud metadata) or unusable addresses
		if neverReachable(ip) {
			return fmt.Errorf("refusing to connect to %s", ip)
		}
		if !allowPrivate && !PublicIP(ip) {
			return fmt.Errorf("refusing to connect to non-public address %s (add the host to the callback allow-list for internal systems)", ip)
		}
		return nil
	}}
	return dialer.DialContext
}

var blockedNets = func() []*net.IPNet {
	var nets []*net.IPNet
	for _, cidr := range []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24",
		"192.168.0.0/16", "198.18.0.0/15", "224.0.0.0/4", "240.0.0.0/4", "::/128", "::1/128", "fc00::/7", "fe80::/10",
		"ff00::/8", "64:ff9b::/96", "64:ff9b:1::/48", "192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24",
		"2001:db8::/32", "100::/64",
		// 6to4 and Teredo embed IPv4 addresses that could point inside the network
		"2002::/16", "2001::/32",
	} {
		_, n, _ := net.ParseCIDR(cidr)
		nets = append(nets, n)
	}
	return nets
}()

var neverNets = func() []*net.IPNet {
	var nets []*net.IPNet
	for _, cidr := range []string{"169.254.0.0/16", "fe80::/10", "0.0.0.0/8", "::/128", "224.0.0.0/4", "ff00::/8", "240.0.0.0/4"} {
		_, n, _ := net.ParseCIDR(cidr)
		nets = append(nets, n)
	}
	return nets
}()

func neverReachable(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	for _, n := range neverNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// PublicIP reports whether ip is a globally routable address.
func PublicIP(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return false
		}
	}
	return true
}
