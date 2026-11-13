package cmd_test

import (
	"strings"
	"testing"
)

func TestDocsOfflineMissesCLI(t *testing.T) {
	isolated(t)
	for _, args := range [][]string{
		{"--library", "django@5.2", "async ORM transactions"},
		{"--library", "https://docs.python.org/3.13/", "TaskGroup cancellation"},
		{"--resolve", "http"},
		{"--section", "https://docs.python.org/3.13/library/asyncio-task.html#task-groups"},
	} {
		call := append([]string{"docs", "--offline", "--json"}, args...)
		if code, out, stderr := cli(t, call...); code != 3 {
			t.Errorf("%v: exit %d, %s, %s", call, code, out, stderr)
		}
	}
	if code, _, stderr := cli(t, "docs", "--offline", "-b", "context7", "transactions"); code != 2 || !strings.Contains(stderr, "--offline") {
		t.Errorf("offline Context7: exit %d, %s", code, stderr)
	}
}
