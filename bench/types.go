// Command bench measures extraction against pinned, independently annotated pages.
package main

import "time"

const schemaVersion = 1
const maxInputBytes = 20 << 20
const maxCorpusBytes = 1 << 30 // Admission limit, not resident memory; fixtures are loaded per worker.

type corpus struct {
	Version int        `json:"version"`
	Pages   []pageSpec `json:"pages"`
}

type pageSpec struct {
	ID           string      `json:"id"`
	Site         string      `json:"site"`
	Kind         string      `json:"kind"`
	Cohort       string      `json:"cohort,omitempty"`
	URL          string      `json:"url"`
	FinalURL     string      `json:"final_url"`
	CapturedAt   string      `json:"captured_at"`
	HTMLSHA      string      `json:"html_sha256"`
	ReferenceSHA string      `json:"reference_sha256"`
	Selector     string      `json:"reference_selector"`
	Exclude      string      `json:"reference_exclude"`
	Review       string      `json:"annotation_notes"`
	Checks       []assertion `json:"checks"`
}

type assertion struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"` // text, absent, heading, code, link, table
	Critical bool     `json:"critical,omitempty"`
	Text     string   `json:"text,omitempty"`
	Headers  []string `json:"headers,omitempty"`
	Cells    []string `json:"cells,omitempty"`
}

type fixture struct {
	Spec           pageSpec
	Dir            string // Nonempty for pinned fixtures loaded on demand; tests may supply HTML directly.
	HTML           []byte
	Reference      string
	ReferenceWords int // Retained for failures and cancellation without loading the source again.
}

type checkResult struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Critical bool   `json:"critical"`
	Passed   bool   `json:"passed"`
}

type accuracy struct {
	TokenRecall    float64       `json:"token_recall"`
	TokenPrecision float64       `json:"token_precision"`
	TokenF1        float64       `json:"token_f1"`
	ReferenceWords int           `json:"reference_tokens"`
	OutputWords    int           `json:"output_tokens"`
	Checks         []checkResult `json:"checks"`
}

type pageResult struct {
	ID            string    `json:"id"`
	Site          string    `json:"site"`
	Kind          string    `json:"kind"`
	Cohort        string    `json:"cohort,omitempty"`
	URL           string    `json:"url"`
	InputBytes    int       `json:"input_bytes"`
	OutputBytes   int       `json:"output_bytes"`
	OutputSHA     string    `json:"output_sha256"`
	SamplesMS     []float64 `json:"samples_ms"`
	MedianMS      float64   `json:"median_ms"`
	P95MS         float64   `json:"p95_ms"`
	Errors        []string  `json:"errors"`
	Deterministic bool      `json:"deterministic"`
	Accuracy      accuracy  `json:"accuracy"`
}

type summary struct {
	Pages          int     `json:"pages"`
	FailedPages    int     `json:"failed_pages"`
	ChecksPassed   int     `json:"checks_passed"`
	ChecksTotal    int     `json:"checks_total"`
	CriticalPassed int     `json:"critical_passed"`
	CriticalTotal  int     `json:"critical_total"`
	MacroRecall    float64 `json:"macro_token_recall"`
	MacroPrecision float64 `json:"macro_token_precision"`
	MacroF1        float64 `json:"macro_token_f1"`
}

type report struct {
	Version     int                `json:"version"`
	Timestamp   string             `json:"timestamp"`
	CorpusSHA   string             `json:"corpus_sha256"`
	BinarySHA   string             `json:"binary_sha256"`
	GoVersion   string             `json:"go_version"`
	CGOEnabled  string             `json:"cgo_enabled"`
	Platform    string             `json:"platform"`
	CPUs        int                `json:"cpus"`
	Mode        string             `json:"mode"`
	ExtractMode string             `json:"extract_mode,omitempty"`
	Iterations  int                `json:"iterations"`
	Warmup      int                `json:"warmup"`
	Workers     int                `json:"workers"`
	WallSeconds float64            `json:"wall_seconds"`
	Summary     summary            `json:"summary"`
	ByKind      map[string]summary `json:"by_kind"`
	ByCohort    map[string]summary `json:"by_cohort,omitempty"`
	Pages       []pageResult       `json:"pages"`
}

type options struct {
	Dir          string
	Archive      string
	Binary       string
	Out          string
	Baseline     string
	Mode         string
	ExtractMode  string
	Iterations   int
	Warmup       int
	Workers      int
	Timeout      time.Duration
	Strict       bool
	SpeedRatio   float64
	AccuracyDrop float64
}

// suffix distinguishes a non-default extraction mode in output and baseline
// paths, so the two modes never overwrite each other's runs.
func (o options) suffix() string {
	if o.ExtractMode == "complete" {
		return ""
	}
	return "-" + o.ExtractMode
}
