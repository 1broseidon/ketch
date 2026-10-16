package health

import "testing"

func TestProbeKeyPoolGroupsKeysThatFailTheSameWay(t *testing.T) {
	for _, tc := range []struct {
		name   string
		keys   []string
		fail   map[string]string // key → detail; absent keys pass
		status Status
		detail string
	}{
		{"one key", []string{"a"}, map[string]string{"a": "bad"}, StatusMisconfigured, "key 1 of 1: bad"},
		{"every key alike", []string{"a", "b", "c"}, map[string]string{"a": "hint", "b": "hint", "c": "hint"}, StatusMisconfigured, "all 3 keys: hint"},
		{"some keys alike", []string{"a", "b", "c"}, map[string]string{"a": "hint", "c": "hint"}, StatusMisconfigured, "keys 1, 3 of 3: hint"},
		{"different details", []string{"a", "b", "c"}, map[string]string{"a": "one", "b": "two", "c": "one"}, StatusMisconfigured, "keys 1, 3 of 3: one; key 2 of 3: two"},
		{"all pass", []string{"a", "b"}, nil, StatusOK, "2 keys accepted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, detail := ProbeKeyPool(tc.keys, func(key string) (Status, string) {
				if d, ok := tc.fail[key]; ok {
					return StatusMisconfigured, d
				}
				return StatusOK, ""
			})
			if status != tc.status || detail != tc.detail {
				t.Fatalf("ProbeKeyPool = %s (%q), want %s (%q)", status, detail, tc.status, tc.detail)
			}
		})
	}
}
