package cmd

import "testing"

func TestIsPseudoVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want bool
	}{
		{"0.12.0", false},
		{"0.12.0-rc.1", false},
		{"0.12.1-0.20260717032048-ebb36335bf7a", true},
		{"0.0.0-20260717032048-ebb36335bf7a", true},
		{"1.2.3-beta", false},
	}
	for _, tc := range cases {
		if got := isPseudoVersion(tc.in); got != tc.want {
			t.Fatalf("isPseudoVersion(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
