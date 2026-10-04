package config

import (
	"encoding/json"
	"errors"

	"github.com/1broseidon/ketch/httpx"
)

// ParseHTTPHeaders parses and validates an http_headers JSON object, as given
// to `ketch config set` or KETCH_HTTP_HEADERS:
// {"https://searx.example": {"CF-Access-Client-Id": "…"}}. Errors never
// include a header value.
func ParseHTTPHeaders(v string) (map[string]map[string]string, error) {
	var headers map[string]map[string]string
	if err := json.Unmarshal([]byte(v), &headers); err != nil || headers == nil {
		return nil, errors.New(`must be a JSON object of origin → {header: value}, e.g. {"https://searx.example":{"CF-Access-Client-Id":"…"}}`)
	}
	if err := httpx.ValidateOriginHeaders(headers); err != nil {
		return nil, err
	}
	return headers, nil
}
