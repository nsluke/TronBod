// Package parse is a tiny REST client for Parse Server. It speaks just enough
// to log in with username/password, query classes, and survive expired
// session tokens by re-logging in once on 401/403.
package parse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

type Config struct {
	BaseURL    string
	AppID      string
	ClientKey  string
	UserAgent  string
	Email      string
	Password   string
	HTTPClient *http.Client
	Session    SessionStore
	Logger     *slog.Logger
}

// SessionStore persists the session token between process restarts. The disk
// implementation lives in session.go; tests can supply a memory store.
type SessionStore interface {
	Load() (string, error)
	Save(token string) error
	Clear() error
}

type Client struct {
	cfg Config

	mu      sync.Mutex
	session string
}

// HasSession reports whether the client has a session token (loaded from
// disk or from a prior Login).
func (c *Client) HasSession() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.session != ""
}

func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New("parse: BaseURL is required")
	}
	if cfg.AppID == "" {
		return nil, errors.New("parse: AppID is required")
	}
	if cfg.ClientKey == "" {
		return nil, errors.New("parse: ClientKey is required")
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	c := &Client{cfg: cfg}
	if cfg.Session != nil {
		if tok, err := cfg.Session.Load(); err == nil {
			c.session = tok
		}
	}
	return c, nil
}

// Login exchanges username + password for a session token. Parse's login
// endpoint is GET /parse/login but POST is also accepted; we use POST so the
// password isn't in a URL.
func (c *Client) Login(ctx context.Context) error {
	body, _ := json.Marshal(map[string]string{
		"username": c.cfg.Email,
		"password": c.cfg.Password,
	})
	req, err := c.newRequest(ctx, http.MethodPost, "/parse/login", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("parse login: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("parse login: %s: %s", resp.Status, truncate(string(b), 256))
	}
	var out struct {
		SessionToken string `json:"sessionToken"`
		ObjectID     string `json:"objectId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("parse login: decode: %w", err)
	}
	if out.SessionToken == "" {
		return errors.New("parse login: empty sessionToken")
	}
	c.mu.Lock()
	c.session = out.SessionToken
	c.mu.Unlock()
	if c.cfg.Session != nil {
		if err := c.cfg.Session.Save(out.SessionToken); err != nil {
			c.cfg.Logger.Warn("parse: session save failed", "err", err)
		}
	}
	c.cfg.Logger.Info("parse: logged in", "user_id", out.ObjectID)
	return nil
}

// QueryParams maps to the standard Parse REST query options.
type QueryParams struct {
	Where   map[string]any
	Order   string
	Limit   int
	Skip    int
	Include []string
}

func (q QueryParams) values() (url.Values, error) {
	v := url.Values{}
	if len(q.Where) > 0 {
		b, err := json.Marshal(q.Where)
		if err != nil {
			return nil, err
		}
		v.Set("where", string(b))
	}
	if q.Order != "" {
		v.Set("order", q.Order)
	}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.Skip > 0 {
		v.Set("skip", strconv.Itoa(q.Skip))
	}
	if len(q.Include) > 0 {
		// Parse expects a comma-separated list.
		s := q.Include[0]
		for _, x := range q.Include[1:] {
			s += "," + x
		}
		v.Set("include", s)
	}
	return v, nil
}

// QueryResult is the standard Parse class-query response.
type QueryResult struct {
	Results []map[string]any `json:"results"`
}

// Query fetches a class. On 401/403 it re-logs in once and retries.
func (c *Client) Query(ctx context.Context, class string, q QueryParams) (*QueryResult, error) {
	v, err := q.values()
	if err != nil {
		return nil, err
	}
	path := "/parse/classes/" + url.PathEscape(class)
	if enc := v.Encode(); enc != "" {
		path += "?" + enc
	}

	body, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var out QueryResult
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("parse query %s: decode: %w", class, err)
	}
	return &out, nil
}

// do executes a request, retrying once after a fresh login if the server
// returns an auth error.
func (c *Client) do(ctx context.Context, method, path string, body io.Reader) ([]byte, error) {
	bodyBytes, err := readAll(body)
	if err != nil {
		return nil, err
	}

	resp, err := c.send(ctx, method, path, bodyBytes)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		resp.Body.Close()
		c.cfg.Logger.Info("parse: session expired, re-logging in")
		if c.cfg.Session != nil {
			_ = c.cfg.Session.Clear()
		}
		c.mu.Lock()
		c.session = ""
		c.mu.Unlock()
		if err := c.Login(ctx); err != nil {
			return nil, err
		}
		resp, err = c.send(ctx, method, path, bodyBytes)
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
		return nil, fmt.Errorf("parse %s %s: %s: %s", method, path, resp.Status, truncate(string(b), 256))
	}
	return b, nil
}

func (c *Client) send(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := c.newRequest(ctx, method, path, r)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.mu.Lock()
	tok := c.session
	c.mu.Unlock()
	if tok != "" {
		req.Header.Set("X-Parse-Session-Token", tok)
	}
	return c.cfg.HTTPClient.Do(req)
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Parse-Application-Id", c.cfg.AppID)
	req.Header.Set("X-Parse-Client-Key", c.cfg.ClientKey)
	if c.cfg.UserAgent != "" {
		req.Header.Set("User-Agent", c.cfg.UserAgent)
	}
	return req, nil
}

func readAll(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	return io.ReadAll(r)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
