package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/1broseidon/ketch/docs"
	"github.com/spf13/pflag"
)

// indexCLI runs `ketch docs index args...` in-process, parsing args with
// the command's own flags, and returns its stdout and error.
func indexCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	reset := func() {
		for _, fs := range []*pflag.FlagSet{docsIndexCmd.Flags(), rootCmd.PersistentFlags()} {
			fs.VisitAll(func(f *pflag.Flag) {
				if sv, ok := f.Value.(pflag.SliceValue); ok {
					_ = sv.Replace(nil)
				} else {
					_ = f.Value.Set(f.DefValue)
				}
				f.Changed = false
			})
		}
	}
	reset()
	t.Cleanup(reset)
	if err := docsIndexCmd.ParseFlags(args); err != nil {
		t.Fatalf("parse %q: %v", args, err)
	}
	prev := docsIndexCmd.Context()
	docsIndexCmd.SetContext(context.Background())
	t.Cleanup(func() { docsIndexCmd.SetContext(prev) })
	var runErr error
	out := captureStdout(t, func() { runErr = runDocsIndex(docsIndexCmd, docsIndexCmd.Flags().Args()) })
	return out, runErr
}

// docs index resolves a name exactly as --resolve does, over the recorded
// registries and sites: an ambiguous name is refused with every candidate
// (exit 2), and the agent passes the one it means; a spec that settles it
// is resolved, indexed from the site's recorded pages, and then answered
// offline by ketch docs under that spec, with the resolution's evidence
// kept.
func TestDocsIndexResolvesLikeAQuery(t *testing.T) {
	ft := widgetFixtures(t)
	docsEnv(t, fixtureFallback{ft})

	_, err := indexCLI(t, "widget")
	matches, _ := resolveJSON(t, "-b", "search", "widget")
	checkAmbiguous(t, []string{"docs", "index", "widget"}, err, matches)

	out, err := indexCLI(t, "--json", "pypi:widget")
	if err != nil {
		t.Fatalf("index pypi:widget: exit %d: %v", exitCode(err), err)
	}
	var report docs.IndexReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("report %q: %v", out, err)
	}
	lib := report.Library
	if !strings.HasPrefix(lib.Root, "https://widget.readthedocs.io/") || lib.Resolved != "resolved" || len(lib.Evidence) == 0 || lib.Pages == 0 {
		t.Fatalf("library = %+v", lib)
	}
	text, err := docsCLI(t, "-b", "search", "--library", "pypi:widget", "configure themes")
	if err != nil {
		t.Fatalf("--library pypi:widget: exit %d: %v", exitCode(err), err)
	}
	if !strings.Contains(text, "backend: search") || !strings.Contains(text, "source: https://widget.readthedocs.io/") {
		t.Errorf("local answer:\n%s", text)
	}
}
