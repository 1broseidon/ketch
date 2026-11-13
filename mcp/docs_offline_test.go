package mcp

import (
	"testing"
)

func TestDocsOfflineMCP(t *testing.T) {
	session := localSession(t)
	for _, args := range []map[string]any{
		{"query": "TaskGroup cancellation", "library": "https://docs.python.org/3.13/", "offline": true},
		{"query": "http", "resolve": true, "offline": true},
		{"section": "https://docs.python.org/3.13/library/asyncio-task.html#task-groups", "offline": true},
		{"query": "transactions", "backend": "context7", "offline": true},
	} {
		res, _ := callDocsOutput(t, session, args)
		if !res.IsError {
			t.Errorf("offline miss did not fail: %v, %+v", args, res)
		}
	}
	res, out := callDocsOutput(t, session, map[string]any{"query": "validation rules", "library": "example", "offline": true})
	if res.IsError || len(out.Results) == 0 {
		t.Fatalf("indexed offline answer failed: %+v, %+v", res, out)
	}
}
