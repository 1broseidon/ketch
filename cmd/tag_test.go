package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// newTagFlagCmd builds a command carrying the flags validateTagFlag inspects.
// withNoCache mirrors scrape/crawl; without it mirrors search, which has no
// --no-cache flag at all — the validator must cope with both.
func newTagFlagCmd(withNoCache bool) *cobra.Command {
	c := &cobra.Command{Use: "fake"}
	c.Flags().String("tag", "", "")
	if withNoCache {
		c.Flags().Bool("no-cache", false, "")
	}
	return c
}

func TestValidateTagFlag(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		withNoCache bool
		args        []string
		wantCode    int
		wantMsg     string
	}{
		{name: "no tag is fine", withNoCache: true},
		{name: "plain tag", withNoCache: true, args: []string{"--tag", "guacamole"}},
		{name: "tag on a command without --no-cache", args: []string{"--tag", "guacamole"}},
		{
			name: "tag with a slash or dot is allowed", withNoCache: true,
			args: []string{"--tag", "proj/auth.v2"},
		},
		{
			name: "tag and no-cache conflict", withNoCache: true,
			args:     []string{"--tag", "guacamole", "--no-cache"},
			wantCode: ExitValidation, wantMsg: "mutually exclusive",
		},
		{
			name: "control characters rejected", withNoCache: true,
			args:     []string{"--tag", "a\tb"},
			wantCode: ExitValidation, wantMsg: "control characters",
		},
		{
			name: "padded name rejected", withNoCache: true,
			args:     []string{"--tag", " guacamole "},
			wantCode: ExitValidation, wantMsg: "empty or padded",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := newTagFlagCmd(tc.withNoCache)
			if err := c.ParseFlags(tc.args); err != nil {
				t.Fatalf("ParseFlags(%q): %v", tc.args, err)
			}
			err := validateTagFlag(c, nil)
			if tc.wantCode == 0 {
				if err != nil {
					t.Fatalf("validateTagFlag(%q) = %v, want nil", tc.args, err)
				}
				return
			}
			var exitErr *ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("validateTagFlag(%q) = %v, want an ExitError", tc.args, err)
			}
			if exitErr.Code != tc.wantCode {
				t.Errorf("exit code = %d, want %d", exitErr.Code, tc.wantCode)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error = %q, want it to mention %q", err, tc.wantMsg)
			}
		})
	}
}

// A nil *tagWriter is the no-tag case, and every fetch path calls record
// unconditionally — so it must be safe on nil, both receiver and page.
func TestTagWriterNilIsANoOp(t *testing.T) {
	t.Parallel()
	if tw := newTagWriter("", nil); tw != nil {
		t.Errorf("newTagWriter with no tag = %v, want nil", tw)
	}
	// A cache that could not be opened yields no writer rather than a panic.
	if tw := newTagWriter("guacamole", nil); tw != nil {
		t.Errorf("newTagWriter with no cache = %v, want nil", tw)
	}
	var nilWriter *tagWriter
	nilWriter.record(nil, "https://example.com", nil) // must not panic
}
