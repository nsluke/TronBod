// Package fitbodapi is a small client for Fitbod's REST API:
//
//   - nautilus.fitbod.me (JSON:API for catalog data)
//   - metros.fitbod.me   (custom REST for user state)
//   - gympulse.fitbod.me (gym/club data)
//   - billing.fitbod.me  (subscriptions)
//   - blimp.fitbod.me    (telemetry config)
//   - pyserve.fitbod.me  (compute functions)
//
// Auth model (captured 2026-04-28 with a fresh login):
//
//   - User logs in via POST gate-keeper.fitbod.me/users/login. The response
//     header `Authorization: Bearer <jwt>` contains the master refresh_token.
//     We don't do this step in code — the user captures the refresh_token
//     once via mitmproxy and stores it in env (FITBOD_REFRESH_TOKEN).
//   - Every host has POST <host>/access_token taking {refresh_token} and
//     returning {access_token: <host-scoped JWT>}. The access_token's `aud`
//     /`iss` claims are host-specific, so a token minted for nautilus is
//     rejected by metros.
//   - Subsequent requests to <host> use Authorization: Bearer <access_token>
//     for that host.
//
// All Fitbod hosts sit behind Cloudflare's bot WAF, which blocks Go's stock
// TLS+HTTP/2 fingerprint at layer 7. We use bogdanfinn/tls-client with the
// Okhttp4Android13 profile (matching the real Fitbod app) to pass.
package fitbodapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"sync"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

// Default backend base URLs. Override per-host via Config.Backends.
var DefaultBackends = map[string]string{
	"nautilus":    "https://nautilus.fitbod.me",
	"metros":      "https://metros.fitbod.me",
	"gympulse":    "https://gympulse.fitbod.me",
	"billing":     "https://billing.fitbod.me",
	"blimp":       "https://blimp.fitbod.me",
	"pyserve":     "https://pyserve.fitbod.me",
	"gate-keeper": "https://gate-keeper.fitbod.me",
}

type Config struct {
	// Backends maps host name → base URL. Missing entries fall back to
	// DefaultBackends.
	Backends map[string]string

	// RefreshToken is the long-lived JWT minted by Fitbod's mobile auth
	// flow. Capture once via mitmproxy and store in env.
	RefreshToken string

	UserAgent string

	// HTTPClient lets callers inject a pre-built tls-client.
	// Default: a fresh client with the Okhttp4Android13 profile.
	HTTPClient tls_client.HttpClient

	Logger *slog.Logger
}

type Client struct {
	cfg      Config
	backends map[string]string

	mu           sync.Mutex
	accessTokens map[string]string // host → host-scoped access_token
}

func New(cfg Config) (*Client, error) {
	if cfg.RefreshToken == "" {
		return nil, errors.New("fitbodapi: RefreshToken is required")
	}
	if cfg.HTTPClient == nil {
		hc, err := newAndroidClient()
		if err != nil {
			return nil, fmt.Errorf("fitbodapi: build tls-client: %w", err)
		}
		cfg.HTTPClient = hc
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	backends := make(map[string]string, len(DefaultBackends))
	for k, v := range DefaultBackends {
		backends[k] = v
	}
	for k, v := range cfg.Backends {
		if v != "" {
			backends[k] = v
		}
	}
	return &Client{
		cfg:          cfg,
		backends:     backends,
		accessTokens: make(map[string]string),
	}, nil
}

func newAndroidClient() (tls_client.HttpClient, error) {
	options := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(30),
		tls_client.WithClientProfile(profiles.Okhttp4Android13),
	}
	return tls_client.NewHttpClient(tls_client.NewNoopLogger(), options...)
}

// Get fetches `host` + `path` with the supplied query string, decoding the
// JSON response body into out. On 401 we re-mint that host's access_token
// and retry once.
func (c *Client) Get(ctx context.Context, host, path string, query url.Values, out any) error {
	base, ok := c.backends[host]
	if !ok {
		return fmt.Errorf("fitbodapi: unknown backend %q", host)
	}
	full := base + path
	if enc := query.Encode(); enc != "" {
		full += "?" + enc
	}

	body, err := c.do(ctx, http.MethodGet, host, full, nil)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("fitbodapi: decode %s: %w", path, err)
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, host, full string, body []byte) ([]byte, error) {
	resp, err := c.send(ctx, method, host, full, body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		c.cfg.Logger.Info("fitbodapi: 401, re-minting access token", "host", host)
		c.clearToken(host)
		if err := c.mintToken(ctx, host); err != nil {
			return nil, fmt.Errorf("fitbodapi: re-mint %s after 401: %w", host, err)
		}
		resp, err = c.send(ctx, method, host, full, body)
		if err != nil {
			return nil, err
		}
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("fitbodapi: %s %s: %s: %s", method, full, resp.Status, truncate(string(b), 256))
	}
	return b, nil
}

func (c *Client) send(ctx context.Context, method, host, full string, body []byte) (*http.Response, error) {
	tok, err := c.tokenFor(ctx, host)
	if err != nil {
		return nil, err
	}

	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, full, r)
	if err != nil {
		return nil, err
	}
	if host == "nautilus" {
		req.Header.Set("Accept", "application/vnd.api+json")
		req.Header.Set("Content-Type", "application/vnd.api+json")
	} else {
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
	}
	if c.cfg.UserAgent != "" {
		req.Header.Set("User-Agent", c.cfg.UserAgent)
	}
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	return c.cfg.HTTPClient.Do(req)
}

// tokenFor returns the cached access_token for host, minting one if missing.
func (c *Client) tokenFor(ctx context.Context, host string) (string, error) {
	c.mu.Lock()
	tok := c.accessTokens[host]
	c.mu.Unlock()
	if tok != "" {
		return tok, nil
	}
	if err := c.mintToken(ctx, host); err != nil {
		return "", err
	}
	c.mu.Lock()
	tok = c.accessTokens[host]
	c.mu.Unlock()
	return tok, nil
}

func (c *Client) clearToken(host string) {
	c.mu.Lock()
	delete(c.accessTokens, host)
	c.mu.Unlock()
}

// mintToken POSTs <host>/access_token with the master refresh_token and
// caches the host-scoped access_token. The /access_token endpoint takes no
// bearer auth itself — only the refresh_token in the body.
func (c *Client) mintToken(ctx context.Context, host string) error {
	base, ok := c.backends[host]
	if !ok {
		return fmt.Errorf("fitbodapi: unknown backend %q", host)
	}
	body, _ := json.Marshal(map[string]string{"refresh_token": c.cfg.RefreshToken})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/access_token", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.UserAgent != "" {
		req.Header.Set("User-Agent", c.cfg.UserAgent)
	}
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("mint %s/access_token: %s: %s", host, resp.Status, truncate(string(b), 512))
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("mint %s/access_token: decode: %w", host, err)
	}
	if out.AccessToken == "" {
		return fmt.Errorf("mint %s/access_token: empty access_token in response", host)
	}
	c.mu.Lock()
	c.accessTokens[host] = out.AccessToken
	c.mu.Unlock()
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
