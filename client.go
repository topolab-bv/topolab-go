package topolab

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Named API environments. Production ships as the default.
const (
	envProduction = "https://api.topolab.nl"
	envStaging    = "https://api-staging.topolab.nl"
)

var environments = map[string]string{
	"production": envProduction,
	"staging":    envStaging,
}

type config struct {
	apiKey      string
	baseURL     string
	environment string
	timeout     time.Duration
	maxRetries  int
	httpClient  *http.Client
	userAgent   string
	backoffBase time.Duration
}

// Option configures a Client. Pass options to New.
type Option func(*config)

// WithAPIKey sets the organization-scoped API key. Defaults to $TOPOLAB_API_KEY.
func WithAPIKey(key string) Option { return func(c *config) { c.apiKey = key } }

// WithBaseURL sets an explicit API base URL, overriding the environment. Use for
// self-hosting or tests.
func WithBaseURL(u string) Option { return func(c *config) { c.baseURL = u } }

// WithEnvironment selects a named environment: "production" (default) or
// "staging". An explicit WithBaseURL takes precedence.
func WithEnvironment(env string) Option { return func(c *config) { c.environment = env } }

// WithTimeout sets the per-request timeout (default 60s). Ignored if
// WithHTTPClient is supplied.
func WithTimeout(d time.Duration) Option { return func(c *config) { c.timeout = d } }

// WithMaxRetries sets the number of retries after the first attempt for
// transient failures (429/5xx/network). Default 3.
func WithMaxRetries(n int) Option { return func(c *config) { c.maxRetries = n } }

// WithHTTPClient supplies a custom *http.Client (proxies, transports, timeouts).
func WithHTTPClient(h *http.Client) Option { return func(c *config) { c.httpClient = h } }

// WithUserAgent overrides the default User-Agent.
func WithUserAgent(ua string) Option { return func(c *config) { c.userAgent = ua } }

// Client is a Topolab API client. It is safe for concurrent use.
type Client struct {
	t *transport

	// Datasets is the catalog service: Client.Datasets.List(ctx, …).
	Datasets *DatasetsService
}

// New constructs a Client. The API key is taken from WithAPIKey or, if unset,
// the TOPOLAB_API_KEY environment variable; New returns a *Error of
// KindConfiguration if neither is present or the resolved base URL is invalid.
func New(opts ...Option) (*Client, error) {
	cfg := config{
		timeout:     60 * time.Second,
		maxRetries:  3,
		backoffBase: 500 * time.Millisecond,
	}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.apiKey == "" {
		cfg.apiKey = os.Getenv("TOPOLAB_API_KEY")
	}
	if cfg.apiKey == "" {
		return nil, &Error{Kind: KindConfiguration, Message: "no API key: pass WithAPIKey or set TOPOLAB_API_KEY"}
	}

	base, err := resolveBaseURL(cfg.baseURL, cfg.environment)
	if err != nil {
		return nil, err
	}
	if err := validateBaseURL(base); err != nil {
		return nil, err
	}

	hc := cfg.httpClient
	if hc == nil {
		hc = &http.Client{Timeout: cfg.timeout}
	}
	ua := cfg.userAgent
	if ua == "" {
		ua = "topolab-go/" + Version + " (+https://docs.topolab.nl)"
	}

	t := &transport{
		apiKey:      cfg.apiKey,
		baseURL:     strings.TrimRight(base, "/"),
		httpClient:  hc,
		maxRetries:  cfg.maxRetries,
		userAgent:   ua,
		backoffBase: cfg.backoffBase,
	}
	c := &Client{t: t}
	c.Datasets = &DatasetsService{t: t}
	return c, nil
}

// Dataset returns a lazy handle for the dataset with the given slug (its
// table name, e.g. "nl-domino-poi").
func (c *Client) Dataset(slug string) *Dataset {
	return &Dataset{t: c.t, Slug: slug}
}

// BaseURL reports the resolved API base URL.
func (c *Client) BaseURL() string { return c.t.baseURL }

// resolveBaseURL applies the precedence: base_url > environment >
// TOPOLAB_BASE_URL > TOPOLAB_ENV > production.
func resolveBaseURL(baseURL, environment string) (string, error) {
	if baseURL != "" {
		return baseURL, nil
	}
	if environment != "" {
		return environmentURL(environment)
	}
	if v := os.Getenv("TOPOLAB_BASE_URL"); v != "" {
		return v, nil
	}
	if v := os.Getenv("TOPOLAB_ENV"); v != "" {
		return environmentURL(v)
	}
	return envProduction, nil
}

func environmentURL(name string) (string, error) {
	if u, ok := environments[strings.ToLower(name)]; ok {
		return u, nil
	}
	return "", &Error{Kind: KindConfiguration, Message: "unknown environment " + name + " (use production or staging)"}
}

// validateBaseURL rejects URLs that could leak the API key: https only (http
// allowed for loopback), and no embedded credentials.
func validateBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return &Error{Kind: KindConfiguration, Message: "invalid base URL: " + err.Error()}
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return &Error{Kind: KindConfiguration, Message: "base URL must use http(s): " + raw}
	}
	if u.User != nil {
		return &Error{Kind: KindConfiguration, Message: "base URL must not contain credentials (userinfo)"}
	}
	if u.Scheme == "http" && !isLoopback(u.Hostname()) {
		return &Error{Kind: KindConfiguration, Message: "base URL must use https for non-loopback host " + u.Hostname()}
	}
	return nil
}

func isLoopback(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}
