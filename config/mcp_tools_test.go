package config

import (
	"strings"
	"testing"
)

func TestNormalizeMCPTools(t *testing.T) {
	cases := []struct {
		name    string
		in      []string
		want    []string
		wantErr string // empty = no error
	}{
		{"nil means unrestricted", nil, nil, ""},
		{"empty means unrestricted", []string{}, nil, ""},
		{"single tool", []string{"search"}, []string{"search"}, ""},
		{"canonical order regardless of input", []string{"crawl", "docs", "search"}, []string{"search", "docs", "crawl"}, ""},
		{"trims and lowercases", []string{" SCRAPE ", "Code"}, []string{"code", "scrape"}, ""},
		{"unknown rejected", []string{"search", "wiki"}, nil, `unknown tool "wiki" (valid: search, code, docs, scrape, crawl)`},
		{"duplicate rejected", []string{"search", "search"}, nil, `duplicate tool "search" (valid: search, code, docs, scrape, crawl)`},
		{"blank rejected", []string{"search", "  "}, nil, "tool name is blank (valid: search, code, docs, scrape, crawl)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeMCPTools(tc.in)
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("NormalizeMCPTools(%q) error = %v, want %q", tc.in, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeMCPTools(%q): %v", tc.in, err)
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("NormalizeMCPTools(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
