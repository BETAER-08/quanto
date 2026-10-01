package github

import (
	"bytes"
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultBaseURL = "https://api.github.com"
	apiVersion     = "2022-11-28"
	acceptJSON     = "application/vnd.github+json"
	acceptRaw      = "application/vnd.github.raw+json"
	requestTimeout = 30 * time.Second
	perPage        = 100
	errorBodyLimit = 1 << 20
	tokenRefresh   = 5 * time.Minute
	defaultBackoff = 60 * time.Second
)

type Options struct {
	BaseURL    string
	AppID      int64
	PrivateKey []byte
	Version    string
	Now        func() time.Time
	OnResponse func(status int, rateRemaining int)
}

type APIError struct {
	Status           int
	Message          string
	DocumentationURL string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("github api: status %d: %s", e.Status, e.Message)
}

type RateLimitError struct {
	Reset time.Time
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("github api: rate limited until %s", e.Reset.UTC().Format(time.RFC3339))
}

type transport struct {
	base       *url.URL
	http       *http.Client
	userAgent  string
	now        func() time.Time
	onResponse func(int, int)
}

type request struct {
	method string
	url    string
	accept string
	body   any
	auth   string
}

func newTransport(opts Options) (*transport, error) {
	raw := opts.BaseURL
	if raw == "" {
		raw = DefaultBaseURL
	}
	base, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, errors.New("github: base URL must be an absolute http or https URL")
	}
	version := opts.Version
	if version == "" {
		version = "dev"
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &transport{
		base:       base,
		http:       &http.Client{Timeout: requestTimeout},
		userAgent:  "quanto/" + version,
		now:        now,
		onResponse: opts.OnResponse,
	}, nil
}

func (t *transport) endpoint(query url.Values, segments ...string) string {
	u := *t.base
	raw := make([]string, len(segments))
	escaped := make([]string, len(segments))
	for i, s := range segments {
		raw[i] = s
		escaped[i] = url.PathEscape(s)
	}
	u.Path = t.base.Path + "/" + strings.Join(raw, "/")
	u.RawPath = t.base.EscapedPath() + "/" + strings.Join(escaped, "/")
	u.RawQuery = ""
	if query != nil {
		u.RawQuery = query.Encode()
	}
	return u.String()
}

func (t *transport) do(ctx context.Context, req request) (*http.Response, error) {
	var body io.Reader
	if req.body != nil {
		data, err := json.Marshal(req.body)
		if err != nil {
			return nil, fmt.Errorf("github: encode request body: %w", err)
		}
		body = bytes.NewReader(data)
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.method, req.url, body)
	if err != nil {
		return nil, fmt.Errorf("github: build %s request: %w", req.method, err)
	}
	accept := req.accept
	if accept == "" {
		accept = acceptJSON
	}
	httpReq.Header.Set("Accept", accept)
	httpReq.Header.Set("X-GitHub-Api-Version", apiVersion)
	httpReq.Header.Set("User-Agent", t.userAgent)
	if req.auth != "" {
		httpReq.Header.Set("Authorization", req.auth)
	}
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	resp, err := t.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("github: %s %s: %w", req.method, redactURL(req.url), unwrapURLError(err))
	}
	if t.onResponse != nil {
		t.onResponse(resp.StatusCode, rateRemaining(resp.Header))
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer resp.Body.Close()
		return nil, t.responseError(resp)
	}
	return resp, nil
}

func unwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "(invalid url)"
	}
	u.User = nil
	u.RawQuery = ""
	return u.String()
}

func rateRemaining(h http.Header) int {
	v := h.Get("X-RateLimit-Remaining")
	if v == "" {
		return -1
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return -1
	}
	return n
}

func (t *transport) responseError(resp *http.Response) error {
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := strings.TrimSpace(resp.Header.Get("Retry-After"))
		remaining := strings.TrimSpace(resp.Header.Get("X-RateLimit-Remaining"))
		if retryAfter != "" || remaining == "0" {
			return &RateLimitError{Reset: t.resetTime(retryAfter, resp.Header.Get("X-RateLimit-Reset"))}
		}
	}
	apiErr := &APIError{Status: resp.StatusCode}
	data, err := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
	if err == nil {
		var payload struct {
			Message          string `json:"message"`
			DocumentationURL string `json:"documentation_url"`
		}
		if json.Unmarshal(data, &payload) == nil {
			apiErr.Message = payload.Message
			apiErr.DocumentationURL = payload.DocumentationURL
		}
	}
	if apiErr.Message == "" {
		apiErr.Message = http.StatusText(resp.StatusCode)
	}
	return apiErr
}

func (t *transport) resetTime(retryAfter, reset string) time.Time {
	now := t.now()
	if retryAfter != "" {
		if secs, err := strconv.Atoi(retryAfter); err == nil && secs >= 0 {
			return now.Add(time.Duration(secs) * time.Second)
		}
		if at, err := http.ParseTime(retryAfter); err == nil {
			return at
		}
	}
	if reset = strings.TrimSpace(reset); reset != "" {
		if unix, err := strconv.ParseInt(reset, 10, 64); err == nil {
			return time.Unix(unix, 0)
		}
	}
	return now.Add(defaultBackoff)
}

func (t *transport) getJSON(ctx context.Context, rawURL, auth string, out any) (*http.Response, error) {
	resp, err := t.do(ctx, request{method: http.MethodGet, url: rawURL, auth: auth})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return nil, fmt.Errorf("github: decode GET %s: %w", redactURL(rawURL), err)
	}
	return resp, nil
}

func (t *transport) sendJSON(ctx context.Context, method, rawURL, auth string, body, out any) error {
	resp, err := t.do(ctx, request{method: method, url: rawURL, auth: auth, body: body})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			return fmt.Errorf("github: read %s %s: %w", method, redactURL(rawURL), err)
		}
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("github: decode %s %s: %w", method, redactURL(rawURL), err)
	}
	return nil
}

func parseNextLink(header string) (string, bool) {
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(part, ";")
		target := strings.TrimSpace(fields[0])
		if len(target) < 2 || target[0] != '<' || target[len(target)-1] != '>' {
			continue
		}
		for _, param := range fields[1:] {
			name, value, ok := strings.Cut(strings.TrimSpace(param), "=")
			if !ok || strings.TrimSpace(name) != "rel" {
				continue
			}
			for _, rel := range strings.Fields(strings.Trim(strings.TrimSpace(value), `"`)) {
				if rel == "next" {
					return target[1 : len(target)-1], true
				}
			}
		}
	}
	return "", false
}

func (t *transport) nextLink(h http.Header) (string, error) {
	for _, v := range h.Values("Link") {
		link, ok := parseNextLink(v)
		if !ok {
			continue
		}
		u, err := url.Parse(link)
		if err != nil {
			return "", errors.New("github: invalid next page link")
		}
		if u.Scheme != t.base.Scheme || u.Host != t.base.Host {
			return "", errors.New("github: next page link points to a different host")
		}
		return u.String(), nil
	}
	return "", nil
}

func paginate[T any](ctx context.Context, t *transport, first, auth string, limit int, extract func(*http.Response) ([]T, error)) ([]T, error) {
	var out []T
	next := first
	for next != "" {
		resp, err := t.do(ctx, request{method: http.MethodGet, url: next, auth: auth})
		if err != nil {
			return nil, err
		}
		items, err := extract(resp)
		closeErr := resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("github: decode GET %s: %w", redactURL(next), err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("github: close GET %s: %w", redactURL(next), closeErr)
		}
		out = append(out, items...)
		if limit > 0 && len(out) >= limit {
			return out[:limit], nil
		}
		next, err = t.nextLink(resp.Header)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func decodeList[T any](resp *http.Response) ([]T, error) {
	var items []T
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, err
	}
	return items, nil
}

type AppClient struct {
	t      *transport
	appID  int64
	key    *rsa.PrivateKey
	mu     sync.Mutex
	tokens map[int64]*tokenEntry
}

type tokenEntry struct {
	mu        sync.Mutex
	token     string
	expiresAt time.Time
	removed   bool
}

func NewAppClient(opts Options) (*AppClient, error) {
	if opts.AppID <= 0 {
		return nil, errors.New("github: app ID must be positive")
	}
	key, err := ParsePrivateKey(opts.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("github: %w", err)
	}
	t, err := newTransport(opts)
	if err != nil {
		return nil, err
	}
	return &AppClient{t: t, appID: opts.AppID, key: key, tokens: map[int64]*tokenEntry{}}, nil
}

func (c *AppClient) jwtAuth() (string, error) {
	token, err := signJWT(c.key, c.appID, c.t.now())
	if err != nil {
		return "", fmt.Errorf("github: %w", err)
	}
	return "Bearer " + token, nil
}

type App struct {
	ID   int64  `json:"id"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

func (c *AppClient) App(ctx context.Context) (*App, error) {
	auth, err := c.jwtAuth()
	if err != nil {
		return nil, err
	}
	var app App
	if _, err := c.t.getJSON(ctx, c.t.endpoint(nil, "app"), auth, &app); err != nil {
		return nil, err
	}
	return &app, nil
}

func (c *AppClient) Installation(ctx context.Context, installationID int64) (*Client, error) {
	if installationID <= 0 {
		return nil, errors.New("github: installation ID must be positive")
	}
	client := &Client{t: c.t, app: c, installationID: installationID}
	if _, err := c.installationToken(ctx, installationID); err != nil {
		return nil, err
	}
	return client, nil
}

func (c *AppClient) entry(installationID int64) *tokenEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.tokens[installationID]
	if !ok {
		e = &tokenEntry{}
		c.tokens[installationID] = e
	}
	return e
}

func (c *AppClient) installationToken(ctx context.Context, installationID int64) (string, error) {
	for {
		e := c.entry(installationID)
		e.mu.Lock()
		if e.removed {
			e.mu.Unlock()
			continue
		}
		if e.token != "" && c.t.now().Before(e.expiresAt.Add(-tokenRefresh)) {
			token := e.token
			e.mu.Unlock()
			return token, nil
		}
		token, err := c.refreshToken(ctx, installationID, e)
		e.mu.Unlock()
		if err != nil {
			return "", err
		}
		c.PruneTokens()
		return token, nil
	}
}

func (c *AppClient) refreshToken(ctx context.Context, installationID int64, e *tokenEntry) (string, error) {
	auth, err := c.jwtAuth()
	if err != nil {
		return "", err
	}
	var payload struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	endpoint := c.t.endpoint(nil, "app", "installations", strconv.FormatInt(installationID, 10), "access_tokens")
	if err := c.t.sendJSON(ctx, http.MethodPost, endpoint, auth, nil, &payload); err != nil {
		return "", fmt.Errorf("github: create installation token: %w", err)
	}
	if payload.Token == "" {
		return "", errors.New("github: create installation token: empty token in response")
	}
	e.token = payload.Token
	e.expiresAt = payload.ExpiresAt
	return e.token, nil
}

func (c *AppClient) PruneTokens() int {
	now := c.t.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	removed := 0
	for id, e := range c.tokens {
		if !e.mu.TryLock() {
			continue
		}
		if e.token == "" || !now.Before(e.expiresAt) {
			e.removed = true
			delete(c.tokens, id)
			removed++
		}
		e.mu.Unlock()
	}
	return removed
}

type Client struct {
	t              *transport
	app            *AppClient
	installationID int64
}

func (c *Client) auth(ctx context.Context) (string, error) {
	token, err := c.app.installationToken(ctx, c.installationID)
	if err != nil {
		return "", err
	}
	return "token " + token, nil
}
