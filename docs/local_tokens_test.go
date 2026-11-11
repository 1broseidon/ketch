package docs

import "testing"

// The estimate counts what bytes miss: code and symbols cost more tokens
// a byte than prose. Fixed counts are o200k_base's.
func TestEstimateTokens(t *testing.T) {
	for _, c := range []struct {
		text string
		want int // o200k_base
	}{
		{"A param is denoted by a colon `:`. When a route is matched, the value of its params will be exposed as `route.params` in every component.", 34},
		{"```js\nconst routes = [\n  // dynamic segments start with a colon\n  { path: '/users/:id', component: User },\n]\n```", 31},
		{"| pattern | matched path |\n| --- | --- |\n| /users/:username | /users/eduardo |", 23},
		{"quantization_config = BitsAndBytesConfig(load_in_8bit=True)", 15},
	} {
		got := estimateTokens(c.text)
		if diff := float64(got-c.want) / float64(c.want); diff < -0.15 || diff > 0.15 {
			t.Errorf("estimateTokens(%q) = %d, o200k %d", c.text, got, c.want)
		}
	}
	if estimateTokens("") != 0 {
		t.Error("empty text costs tokens")
	}
}
