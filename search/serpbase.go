package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/1broseidon/ketch/health"
	"github.com/1broseidon/ketch/httpx"
	config "github.com/1broseidon/ketch/internal/configbase"
)

// serpbaseEndpoint is the hosted SerpBase Google Search API. The provider
// requires POST JSON with the key in the X-API-Key header — never a query param
// or body field — so transport errors cannot leak the key via a URL.
const serpbaseEndpoint = "https://api.serpbase.dev/google/search"

// SerpBase reports request outcomes in a business `status` field with HTTP 200,
// so errors must be read from the response body, not the HTTP status code.
const (
	serpBaseStatusSuccess           = 0
	serpBaseStatusInvalidRequest    = 1000
	serpBaseStatusUnauthorized      = 1001
	serpBaseStatusInsufficientFunds = 1020
	serpBaseStatusRateLimited       = 1029
)

// SerpBase searches Google via the SerpBase REST API, returning structured
// organic results without any scraping maintenance.
type SerpBase struct {
	keys   keyPool
	client *http.Client
}

// NewSerpBase creates a new SerpBase search backend.
func NewSerpBase(apiKey string) *SerpBase {
	return newSerpBaseWithKeys([]string{apiKey})
}

func newSerpBaseWithKeys(keys []string) *SerpBase {
	return &SerpBase{keys: newKeyPool(keys), client: httpx.Default()}
}

type serpBaseRequest struct {
	Query  string `json:"q"`
	Lang   string `json:"hl"`
	Region string `json:"gl"`
	Page   int    `json:"page"`
}

type serpBasePayload struct {
	Status  int              `json:"status"`
	Error   string           `json:"error"`
	Organic []serpBaseResult `json:"organic"`
}

type serpBaseResult struct {
	Title   string `json:"title"`
	Link    string `json:"link"`
	Snippet string `json:"snippet"`
}

// Search queries SerpBase and returns up to limit organic results.
func (s *SerpBase) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	if limit <= 0 {
		return []Result{}, nil
	}

	key := s.keys.pick()
	attempt, err := s.request(ctx, query, key)
	if err != nil {
		return nil, err
	}
	if attempt.retryable() && s.keys.size() > 1 {
		key = s.keys.pickDifferent(key)
		attempt, err = s.request(ctx, query, key)
		if err != nil {
			return nil, err
		}
	}
	if err := attempt.err(s.keys.keyLabel(key)); err != nil {
		return nil, err
	}

	results := make([]Result, 0, limit)
	for _, r := range attempt.payload.Organic {
		if len(results) >= limit {
			break
		}
		if strings.TrimSpace(r.Link) == "" {
			continue
		}
		results = append(results, Result{
			Title:       r.Title,
			URL:         r.Link,
			Description: r.Snippet,
		})
	}
	return results, nil
}

// serpBaseAttempt is one completed request: the HTTP status, the decoded
// business payload, and the raw body for diagnostics on non-JSON failures.
type serpBaseAttempt struct {
	httpStatus int
	raw        []byte
	payload    serpBasePayload
}

// retryable reports whether another key might succeed. Both the HTTP status and
// the business status are checked so gateway-level 401/429 responses keep
// rotating keys.
func (a serpBaseAttempt) retryable() bool {
	if a.httpStatus == http.StatusUnauthorized || a.httpStatus == http.StatusForbidden || a.httpStatus == http.StatusTooManyRequests {
		return true
	}
	return a.payload.Status == serpBaseStatusUnauthorized || a.payload.Status == serpBaseStatusRateLimited
}

// err maps the attempt to an actionable error, or nil on success. The business
// status takes precedence when present; otherwise the HTTP status is mapped so
// gateway-level 401/402/429 keep their friendly messages.
func (a serpBaseAttempt) err(keyLabel string) error {
	if a.httpStatus == http.StatusOK && a.payload.Status == serpBaseStatusSuccess {
		return nil
	}
	switch a.payload.Status {
	case serpBaseStatusUnauthorized:
		return fmt.Errorf("serpbase: invalid API key (%s; set via: ketch config set serpbase_api_key <key>)", keyLabel)
	case serpBaseStatusInsufficientFunds:
		return fmt.Errorf("serpbase: search credits exhausted — top up at https://serpbase.dev (%s)", keyLabel)
	case serpBaseStatusRateLimited:
		return fmt.Errorf("serpbase: rate limited (%s)", keyLabel)
	case serpBaseStatusInvalidRequest:
		return fmt.Errorf("serpbase: invalid request: %s", a.errorText())
	}
	switch a.httpStatus {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("serpbase: invalid API key (%s; set via: ketch config set serpbase_api_key <key>)", keyLabel)
	case http.StatusPaymentRequired:
		return fmt.Errorf("serpbase: search credits exhausted — top up at https://serpbase.dev (%s)", keyLabel)
	case http.StatusTooManyRequests:
		return fmt.Errorf("serpbase: rate limited (%s)", keyLabel)
	case http.StatusOK:
		return fmt.Errorf("serpbase returned status %d: %s", a.payload.Status, a.errorText())
	default:
		return fmt.Errorf("serpbase returned status %d%s", a.httpStatus, a.bodyDetail())
	}
}

func (a serpBaseAttempt) bodyDetail() string {
	detail := strings.TrimSpace(string(a.raw))
	if len(detail) > 4096 {
		detail = detail[:4096]
	}
	if detail == "" {
		return ""
	}
	return ": " + detail
}

func (a serpBaseAttempt) errorText() string {
	if msg := strings.TrimSpace(a.payload.Error); msg != "" {
		return msg
	}
	return "unknown error"
}

func (s *SerpBase) request(ctx context.Context, query, key string) (serpBaseAttempt, error) {
	body, err := json.Marshal(serpBaseRequest{Query: query, Lang: "en", Region: "us", Page: 1})
	if err != nil {
		return serpBaseAttempt{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serpbaseEndpoint, bytes.NewReader(body))
	if err != nil {
		return serpBaseAttempt{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-SerpBase-Source", "ketch")
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return serpBaseAttempt{}, fmt.Errorf("serpbase request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return serpBaseAttempt{}, fmt.Errorf("failed to read serpbase response: %w", err)
	}
	attempt := serpBaseAttempt{httpStatus: resp.StatusCode, raw: raw}
	decodeErr := json.Unmarshal(raw, &attempt.payload)
	if resp.StatusCode == http.StatusOK && decodeErr != nil {
		return serpBaseAttempt{}, fmt.Errorf("failed to decode serpbase response: %w", decodeErr)
	}
	return attempt, nil
}

// ProbeSerpBase checks the provider using a caller-supplied client and endpoint.
func ProbeSerpBase(ctx context.Context, client *http.Client, endpoint, apiKey string) (health.Status, string) {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return health.StatusNoKey, "API key not set (get a free key at https://serpbase.dev then: ketch config set serpbase_api_key <key>)"
	}
	body, err := json.Marshal(serpBaseRequest{Query: "ketch", Lang: "en", Region: "us", Page: 1})
	if err != nil {
		return health.StatusUnreachable, health.ErrorDetail(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return health.StatusUnreachable, health.ErrorDetail(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-SerpBase-Source", "ketch")
	req.Header.Set("X-API-Key", key)

	resp, err := client.Do(req)
	if err != nil {
		return health.StatusUnreachable, health.ErrorDetail(err)
	}
	defer health.Drain(resp)

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return health.StatusUnreachable, health.ErrorDetail(err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var payload serpBasePayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			return health.StatusUnreachable, "returned an unreadable response"
		}
		return serpBaseProbeStatus(payload.Status)
	case http.StatusUnauthorized, http.StatusForbidden:
		return health.StatusMisconfigured, "API key rejected (ketch config set serpbase_api_key <key>)"
	case http.StatusTooManyRequests:
		return health.StatusOK, "reachable, key accepted (rate limited)"
	case http.StatusPaymentRequired:
		return health.StatusOK, "reachable, key accepted (search credits exhausted)"
	default:
		return health.StatusUnreachable, fmt.Sprintf("returned status %d", resp.StatusCode)
	}
}

func serpBaseProbeStatus(status int) (health.Status, string) {
	switch status {
	case serpBaseStatusSuccess:
		return health.StatusOK, ""
	case serpBaseStatusUnauthorized:
		return health.StatusMisconfigured, "API key rejected (ketch config set serpbase_api_key <key>)"
	case serpBaseStatusInsufficientFunds:
		return health.StatusOK, "reachable, key accepted (search credits exhausted)"
	case serpBaseStatusRateLimited:
		return health.StatusOK, "reachable, key accepted (rate limited)"
	case serpBaseStatusInvalidRequest:
		return health.StatusUnreachable, "returned status 1000 (invalid request)"
	default:
		return health.StatusUnreachable, fmt.Sprintf("returned status %d", status)
	}
}

func serpbaseProvider() Provider {
	return Provider{
		// SerpBase scrapes Google on demand; probes routinely take 1-3s and can
		// exceed the default doctor budget, so allow a SearXNG-sized timeout.
		MinProbeTimeout: 10 * time.Second,
		Settings:        []config.Setting{config.KeyPool("serpbase_api_key", "serpbase_api_keys", 13, 14, 8, 13)},
		AutoRank:        50,
		ID:              "serpbase",
		Setup:           "serpbase: API key not set (get a free key at https://serpbase.dev then: ketch config set serpbase_api_key <key>)",
		Name:            "SerpBase",
		Usable:          func(c *config.Config) bool { return len(c.SerpBaseKeys()) > 0 },
		New:             func(c *config.Config) (Searcher, error) { return newSerpBaseWithKeys(c.SerpBaseKeys()), nil },
		Probe: func(ctx context.Context, client *http.Client, c *config.Config) (health.Status, string) {
			return health.ProbeKeyPool(c.SerpBaseKeys(), func(key string) (health.Status, string) {
				return ProbeSerpBase(ctx, client, "https://api.serpbase.dev/google/search", key)
			})
		},
	}
}
