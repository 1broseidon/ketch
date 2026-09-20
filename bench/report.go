package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func summarize(pages []pageResult) (summary, map[string]summary) {
	all := summarizeGroup(pages)
	groups := make(map[string][]pageResult)
	for _, page := range pages {
		groups[page.Kind] = append(groups[page.Kind], page)
	}
	byKind := make(map[string]summary)
	for kind, group := range groups {
		byKind[kind] = summarizeGroup(group)
	}
	return all, byKind
}

func summarizeGroup(pages []pageResult) summary {
	s := summary{Pages: len(pages)}
	for _, p := range pages {
		if len(p.Errors) > 0 || !p.Deterministic {
			s.FailedPages++
		}
		s.MacroRecall += p.Accuracy.TokenRecall
		s.MacroPrecision += p.Accuracy.TokenPrecision
		s.MacroF1 += p.Accuracy.TokenF1
		for _, c := range p.Accuracy.Checks {
			s.ChecksTotal++
			if c.Passed {
				s.ChecksPassed++
			}
			if c.Critical {
				s.CriticalTotal++
				if c.Passed {
					s.CriticalPassed++
				}
			}
		}
	}
	if len(pages) > 0 {
		s.MacroRecall /= float64(len(pages))
		s.MacroPrecision /= float64(len(pages))
		s.MacroF1 /= float64(len(pages))
	}
	return s
}

func summarizeCohorts(pages []pageResult) map[string]summary {
	groups := make(map[string][]pageResult)
	for _, page := range pages {
		key := page.Cohort
		if key == "" {
			key = "unspecified"
		}
		groups[key] = append(groups[key], page)
	}
	result := make(map[string]summary)
	for key, group := range groups {
		result[key] = summarizeGroup(group)
	}
	return result
}

func saveReport(r *report, dir string) error {
	if err := writeJSON(filepath.Join(dir, "results.json"), r); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "RESULTS.md"), []byte(markdownReport(r)), 0o644)
}

func markdownReport(r *report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Ketch extraction benchmark\n\n%s · %s · %d logical CPUs\n\n", r.Timestamp, r.Platform, r.CPUs)
	fmt.Fprintf(&b, "Binary build: %s, CGO_ENABLED=%s.\n\n", r.GoVersion, r.CGOEnabled)
	fmt.Fprintf(&b, "Mode: **%s**, extract mode **%s**. %d measured runs + %d warmups per page, %d workers. Wall time %.2fs.\n\n", r.Mode, extractModeOf(r), r.Iterations, r.Warmup, r.Workers, r.WallSeconds)
	fmt.Fprintf(&b, "Corpus SHA256: `%s`  \nBinary SHA256: `%s`\n\n", r.CorpusSHA, r.BinarySHA)
	fmt.Fprintf(&b, "**%d/%d checks passed; %d/%d critical checks; %d failed or nondeterministic pages.**\n\n", r.Summary.ChecksPassed, r.Summary.ChecksTotal, r.Summary.CriticalPassed, r.Summary.CriticalTotal, r.Summary.FailedPages)
	b.WriteString("Token coverage compares independently annotated source text with parsed Markdown. It is a multiset measure, not semantic correctness. Code, headings, table associations, links and exclusions are checked separately. Scores include every page; pages have equal weight in macro averages. Per-invocation timing includes CLI startup and excludes build, source loading, network and grading. Wall time includes warmups, grading and artifact writes. Selector mode is assisted recovery, not default extraction.\n\n")
	fmt.Fprintf(&b, "Macro token recall **%.1f%%**, precision **%.1f%%**, F1 **%.1f%%**.\n\n", 100*r.Summary.MacroRecall, 100*r.Summary.MacroPrecision, 100*r.Summary.MacroF1)
	b.WriteString("| Page | Kind | Recall | Precision | Checks | Critical | Median | p95 | Output |\n|---|---|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, p := range r.Pages {
		s := summarizeGroup([]pageResult{p})
		fmt.Fprintf(&b, "| [%s](%s) | %s | %.1f%% | %.1f%% | %d/%d | %d/%d | %.2fms | %.2fms | %d B |\n", p.ID, p.URL, p.Kind,
			100*p.Accuracy.TokenRecall, 100*p.Accuracy.TokenPrecision, s.ChecksPassed, s.ChecksTotal,
			s.CriticalPassed, s.CriticalTotal, p.MedianMS, p.P95MS, p.OutputBytes)
	}
	writeGroupReport(&b, r)
	writeCohortReport(&b, r.ByCohort)
	writeFailures(&b, r.Pages)
	return b.String()
}

func writeCohortReport(b *strings.Builder, groups map[string]summary) {
	if len(groups) == 0 {
		return
	}
	b.WriteString("\n## By corpus cohort\n\nHeld-out sites were selected and annotated before this evaluation, without tuning extraction on their output. Once inspected, they become regression evidence; future blind evaluations need fresh sites.\n\n| Cohort | Pages | Recall | Precision | Checks | Critical |\n|---|---:|---:|---:|---:|---:|---:|\n")
	var keys []string
	for key := range groups {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		s := groups[key]
		fmt.Fprintf(b, "| %s | %d | %.1f%% | %.1f%% | %d/%d | %d/%d |\n", key, s.Pages, 100*s.MacroRecall, 100*s.MacroPrecision, s.ChecksPassed, s.ChecksTotal, s.CriticalPassed, s.CriticalTotal)
	}
}

func writeGroupReport(b *strings.Builder, r *report) {
	b.WriteString("\n## By content type\n\n| Kind | Pages | Recall | Precision | Checks |\n|---|---:|---:|---:|---:|\n")
	var keys []string
	for k := range r.ByKind {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		s := r.ByKind[k]
		fmt.Fprintf(b, "| %s | %d | %.1f%% | %.1f%% | %d/%d |\n", k, s.Pages, 100*s.MacroRecall, 100*s.MacroPrecision, s.ChecksPassed, s.ChecksTotal)
	}
}

func writeFailures(b *strings.Builder, pages []pageResult) {
	b.WriteString("\n## Failures and omissions\n\n")
	for _, p := range pages {
		for _, err := range p.Errors {
			fmt.Fprintf(b, "- **%s**: %s\n", p.ID, err)
		}
		if !p.Deterministic {
			fmt.Fprintf(b, "- **%s**: output changed between identical-input samples.\n", p.ID)
		}
		for _, c := range p.Accuracy.Checks {
			if !c.Passed {
				fmt.Fprintf(b, "- **%s/%s** (%s, critical=%t)\n", p.ID, c.ID, c.Kind, c.Critical)
			}
		}
	}
}
