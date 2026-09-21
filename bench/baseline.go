package main

import "fmt"

func compareReports(current, baseline *report, opts options) []string {
	if current.Version != baseline.Version || current.CorpusSHA != baseline.CorpusSHA {
		return []string{"corpus or scoring schema changed: review annotations and create a new baseline"}
	}
	if !compatibleRuns(current, baseline) {
		if diffs := incompatibleFields(current, baseline); len(diffs) > 0 {
			return diffs
		}
		return []string{"incompatible mode, extract mode, samples, workers, platform or Go/CGO build: use a matching baseline"}
	}
	var failures []string
	pages := make(map[string]pageResult)
	for _, p := range current.Pages {
		pages[p.ID] = p
	}
	if len(current.Pages) != len(baseline.Pages) {
		failures = append(failures, "page count changed")
	}
	for _, b := range baseline.Pages {
		p, ok := pages[b.ID]
		if !ok {
			failures = append(failures, "missing page: "+b.ID)
			continue
		}
		failures = append(failures, comparePage(p, b, opts)...)
	}
	return failures
}

func compatibleRuns(a, b *report) bool {
	return a.Mode == b.Mode && extractModeOf(a) == extractModeOf(b) && a.Workers == b.Workers && a.Platform == b.Platform && a.CPUs == b.CPUs &&
		a.Iterations == b.Iterations && a.Warmup == b.Warmup && a.GoVersion == b.GoVersion && a.CGOEnabled == b.CGOEnabled
}

// incompatibleFields reports, one line per field, every compatibleRuns
// criterion on which the current run and the baseline run disagree - so a
// failed check names the mismatch (e.g. a CGO_ENABLED=1 baseline compared
// against this runner's CGO_ENABLED=0 self-build) instead of leaving the
// operator to guess which of the nine criteria tripped it.
func incompatibleFields(current, baseline *report) []string {
	fields := []struct {
		name            string
		current, wanted string
	}{
		{"mode", current.Mode, baseline.Mode},
		{"extract_mode", extractModeOf(current), extractModeOf(baseline)},
		{"workers", fmt.Sprintf("%d", current.Workers), fmt.Sprintf("%d", baseline.Workers)},
		{"platform", current.Platform, baseline.Platform},
		{"cpus", fmt.Sprintf("%d", current.CPUs), fmt.Sprintf("%d", baseline.CPUs)},
		{"iterations", fmt.Sprintf("%d", current.Iterations), fmt.Sprintf("%d", baseline.Iterations)},
		{"warmup", fmt.Sprintf("%d", current.Warmup), fmt.Sprintf("%d", baseline.Warmup)},
		{"go_version", current.GoVersion, baseline.GoVersion},
		{"cgo_enabled", current.CGOEnabled, baseline.CGOEnabled},
	}
	var diffs []string
	for _, f := range fields {
		if f.current != f.wanted {
			diffs = append(diffs, fmt.Sprintf("incompatible baseline: %s: baseline %q, current %q", f.name, f.wanted, f.current))
		}
	}
	return diffs
}

func comparePage(p, b pageResult, opts options) []string {
	var failures []string
	if len(p.Errors) > 0 || !p.Deterministic {
		failures = append(failures, p.ID+": execution failure/nondeterminism")
	}
	if p.MedianMS > b.MedianMS*opts.SpeedRatio && p.MedianMS-b.MedianMS > 2 {
		failures = append(failures, fmt.Sprintf("%s: median %.2fms exceeds %.2fms × %.2f", p.ID, p.MedianMS, b.MedianMS, opts.SpeedRatio))
	}
	if p.Accuracy.TokenRecall+opts.AccuracyDrop < b.Accuracy.TokenRecall || p.Accuracy.TokenPrecision+opts.AccuracyDrop < b.Accuracy.TokenPrecision {
		failures = append(failures, p.ID+": token coverage regressed")
	}
	passed := make(map[string]bool)
	for _, c := range p.Accuracy.Checks {
		passed[c.ID] = c.Passed
	}
	for _, c := range b.Accuracy.Checks {
		pass, exists := passed[c.ID]
		if !exists {
			failures = append(failures, p.ID+"/"+c.ID+": missing assertion")
		} else if c.Passed && !pass {
			failures = append(failures, p.ID+"/"+c.ID+": previously passing check regressed")
		}
	}
	if len(p.Accuracy.Checks) != len(b.Accuracy.Checks) {
		failures = append(failures, p.ID+": assertion count changed")
	}
	return failures
}

func strictFailures(r *report) []string {
	var failures []string
	for _, p := range r.Pages {
		for _, c := range p.Accuracy.Checks {
			if c.Critical && !c.Passed {
				failures = append(failures, p.ID+"/"+c.ID+": critical check failed")
			}
		}
	}
	return failures
}

// extractModeOf reads a report's extraction mode; a report written before
// the field existed measured the default.
func extractModeOf(r *report) string {
	if r.ExtractMode == "" {
		return "complete"
	}
	return r.ExtractMode
}
