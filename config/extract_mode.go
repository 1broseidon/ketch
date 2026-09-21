package config

import (
	"fmt"
	"strings"
)

// extractModes lists the values extract_mode accepts, default first: clean
// drops chrome by structure and by name and phrase, complete keeps
// everything the page's structure does not condemn. The extract package owns the
// behaviour behind each name (extract.ParseMode) and cannot be imported
// from here, so the list is repeated; a test in extract keeps the two in
// step.
var extractModes = []string{"clean", "complete"}

// ExtractModes returns a copy of the values extract_mode accepts, default
// first.
func ExtractModes() []string {
	return append([]string(nil), extractModes...)
}

// NormalizeExtractMode validates an operator-configured extract_mode. The
// value is trimmed and lowercased; an empty value means the default and
// returns "", and anything else must be one of ExtractModes.
func NormalizeExtractMode(value string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(value))
	if mode == "" {
		return "", nil
	}
	for _, valid := range extractModes {
		if mode == valid {
			return mode, nil
		}
	}
	return "", fmt.Errorf("unknown extract_mode %q (valid: %s)", value, strings.Join(extractModes, ", "))
}
