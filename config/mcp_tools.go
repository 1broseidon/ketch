package config

import (
	"fmt"
	"strings"
)

// mcpToolNames lists every tool `ketch mcp serve` can publish, in the
// canonical order used for tool registration and server instructions.
// Private on purpose: an externally mutable slice would let importers alter
// validation and registration (or inflate the tool count past countWord's
// table). Exported read-only via MCPToolNames.
var mcpToolNames = []string{"search", "code", "docs", "scrape", "crawl"}

// MCPToolNames returns a copy of the canonical list of tools `ketch mcp
// serve` can publish, in registration order.
func MCPToolNames() []string {
	return append([]string(nil), mcpToolNames...)
}

// NormalizeMCPTools validates an operator-configured mcp_tools allowlist:
// entries are trimmed and lowercased; blank, unknown, and duplicate names are
// rejected (with the valid names in the error); the result is returned in
// canonical order so server instructions stay deterministic regardless of
// input order. A nil or empty list means "no restriction" and returns nil.
func NormalizeMCPTools(list []string) ([]string, error) {
	if len(list) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(list))
	for _, raw := range list {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			return nil, fmt.Errorf("tool name is blank (valid: %s)", strings.Join(mcpToolNames, ", "))
		}
		if _, dup := seen[name]; dup {
			return nil, fmt.Errorf("duplicate tool %q (valid: %s)", name, strings.Join(mcpToolNames, ", "))
		}
		known := false
		for _, valid := range mcpToolNames {
			if name == valid {
				known = true
				break
			}
		}
		if !known {
			return nil, fmt.Errorf("unknown tool %q (valid: %s)", name, strings.Join(mcpToolNames, ", "))
		}
		seen[name] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for _, name := range mcpToolNames {
		if _, ok := seen[name]; ok {
			out = append(out, name)
		}
	}
	return out, nil
}
