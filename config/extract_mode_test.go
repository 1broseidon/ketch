package config

import (
	"strings"
	"testing"
)

func TestNormalizeExtractMode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"", ""}, {"  ", ""}, {"complete", "complete"}, {"clean", "clean"}, {" Clean ", "clean"}, {"COMPLETE", "complete"},
	} {
		got, err := NormalizeExtractMode(tc.in)
		if err != nil || got != tc.want {
			t.Fatalf("NormalizeExtractMode(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []string{"fast", "clean,complete", "readability"} {
		if _, err := NormalizeExtractMode(bad); err == nil || !strings.Contains(err.Error(), "valid: clean, complete") {
			t.Fatalf("NormalizeExtractMode(%q) err = %v; want the valid names", bad, err)
		}
	}
}

func TestExtractModesIsACopy(t *testing.T) {
	t.Parallel()
	modes := ExtractModes()
	if strings.Join(modes, ",") != "clean,complete" {
		t.Fatalf("ExtractModes = %v", modes)
	}
	modes[0] = "mutated"
	if ExtractModes()[0] != "clean" {
		t.Fatal("ExtractModes exposed its backing slice")
	}
}
