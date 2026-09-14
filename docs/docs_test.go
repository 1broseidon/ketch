package docs

import "testing"

func TestTruncate(t *testing.T) {
	in := []Result{{Title: "a"}, {Title: "b"}, {Title: "c"}}
	for _, tc := range []struct{ limit, want int }{{0, 3}, {-1, 3}, {2, 2}, {3, 3}, {9, 3}} {
		if got := len(Truncate(in, tc.limit)); got != tc.want {
			t.Errorf("Truncate(limit=%d) = %d results, want %d", tc.limit, got, tc.want)
		}
	}
	if Truncate(nil, 2) != nil {
		t.Errorf("Truncate(nil) should stay nil")
	}
}
