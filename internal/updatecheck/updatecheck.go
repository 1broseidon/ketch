// Package updatecheck surfaces a soft "newer version available" notice
// when ketch is run interactively. It caches release metadata locally so
// the network check happens at most once per day, and the hint only
// appears when the cached status is recent.
//
// Respect: KETCH_NO_UPDATE_NOTIFIER=1 disables everything; the CI env var
// also suppresses notices automatically.
package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type InstallType string

const (
	InstallUnknown  InstallType = "unknown"
	InstallHomebrew InstallType = "homebrew"
	InstallGo       InstallType = "go"
	InstallManual   InstallType = "manual"
)

const (
	checkTTL      = 24 * time.Hour
	failedRetryTT = 6 * time.Hour
	notifyTTL     = 24 * time.Hour
	schemaVersion = 1
	releaseLatest = "https://api.github.com/repos/1broseidon/ketch/releases/latest"
	releaseURL    = "https://github.com/1broseidon/ketch/releases/latest"
)

type Options struct {
	CurrentVersion string
	AllowNetwork   bool
	Timeout        time.Duration
}

type Status struct {
	CheckedAt     time.Time   `json:"checked_at,omitempty"`
	CacheStale    bool        `json:"cache_stale"`
	Available     bool        `json:"available"`
	LatestVersion string      `json:"latest_version,omitempty"`
	InstallType   InstallType `json:"install_type,omitempty"`
	Command       string      `json:"command,omitempty"`
	ReleaseURL    string      `json:"release_url,omitempty"`
	Source        string      `json:"source,omitempty"`
}

type cacheState struct {
	SchemaVersion     int         `json:"schema_version"`
	CurrentVersion    string      `json:"current_version,omitempty"`
	LastCheckedAt     time.Time   `json:"last_checked_at,omitempty"`
	LastCheckFailedAt time.Time   `json:"last_check_failed_at,omitempty"`
	LatestVersion     string      `json:"latest_version,omitempty"`
	ReleaseURL        string      `json:"release_url,omitempty"`
	UpdateAvailable   bool        `json:"update_available"`
	InstallType       InstallType `json:"install_type,omitempty"`
	UpdateCommand     string      `json:"update_command,omitempty"`
	LastNotifiedAt    time.Time   `json:"last_notified_at,omitempty"`
	LastNotifiedVer   string      `json:"last_notified_version,omitempty"`
}

type releaseInfo struct {
	Version string
	URL     string
}

var (
	nowFn        = time.Now
	cacheDirFn   = os.UserCacheDir
	execPathFn   = os.Executable
	evalSymlinks = filepath.EvalSymlinks
	releaseFetch = fetchLatestRelease
)

// Disabled reports whether the user has opted out of update checks.
func Disabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("KETCH_NO_UPDATE_NOTIFIER")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// GetStatus returns the cached or freshly-fetched update status.
// AllowNetwork=false forces cache-only mode.
func GetStatus(ctx context.Context, opts Options) (Status, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	installType, installCmd := detectInstall()
	status := Status{
		InstallType: installType,
		Command:     installCmd,
		ReleaseURL:  releaseURL,
		Source:      "none",
	}
	if !isVersionCheckable(opts.CurrentVersion) {
		return status, nil
	}

	state, err := loadState()
	hasCache := err == nil && state.SchemaVersion == schemaVersion
	if hasCache {
		status = statusFromState(state, opts.CurrentVersion, installType, installCmd)
		status.CacheStale = isCacheStale(state, nowFn())
		if !status.CacheStale {
			status.Source = "cache"
			return status, nil
		}
		if !opts.AllowNetwork || inFailureBackoff(state, nowFn()) {
			status.Source = "cache"
			return status, nil
		}
	} else if !opts.AllowNetwork {
		return status, nil
	}

	if !opts.AllowNetwork {
		return status, nil
	}

	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	rel, fetchErr := releaseFetch(ctx)
	if fetchErr != nil {
		if hasCache {
			state.LastCheckFailedAt = nowFn()
			_ = saveState(state)
			status = statusFromState(state, opts.CurrentVersion, installType, installCmd)
			status.CacheStale = true
			status.Source = "cache"
			return status, fetchErr
		}
		return status, fetchErr
	}

	state = cacheState{
		SchemaVersion:   schemaVersion,
		CurrentVersion:  opts.CurrentVersion,
		LastCheckedAt:   nowFn(),
		LatestVersion:   rel.Version,
		ReleaseURL:      rel.URL,
		UpdateAvailable: compareVersions(opts.CurrentVersion, rel.Version) < 0,
		InstallType:     installType,
		UpdateCommand:   renderCommand(installType, rel.Version),
	}
	if old, loadErr := loadState(); loadErr == nil {
		state.LastNotifiedAt = old.LastNotifiedAt
		state.LastNotifiedVer = old.LastNotifiedVer
	}
	if saveErr := saveState(state); saveErr != nil {
		return statusFromState(state, opts.CurrentVersion, installType, installCmd), saveErr
	}
	status = statusFromState(state, opts.CurrentVersion, installType, installCmd)
	status.Source = "live"
	return status, nil
}

// ShouldNotify reports whether the passive notice should be shown.
// The notice is suppressed if the same version was already announced
// within the notifyTTL window.
func ShouldNotify(status Status) bool {
	if Disabled() || !status.Available {
		return false
	}
	state, err := loadState()
	if err != nil {
		return true
	}
	if state.LastNotifiedVer != status.LatestVersion {
		return true
	}
	if state.LastNotifiedAt.IsZero() {
		return true
	}
	return nowFn().Sub(state.LastNotifiedAt) >= notifyTTL
}

// MarkNotified records that the user has been shown the current status.
func MarkNotified(status Status) error {
	if !status.Available || status.LatestVersion == "" {
		return nil
	}
	state, err := loadState()
	if err != nil {
		state = cacheState{
			SchemaVersion: schemaVersion,
			LatestVersion: status.LatestVersion,
			ReleaseURL:    status.ReleaseURL,
			InstallType:   status.InstallType,
			UpdateCommand: status.Command,
		}
	}
	state.LastNotifiedAt = nowFn()
	state.LastNotifiedVer = status.LatestVersion
	return saveState(state)
}

// FormatNotice renders the short two-line reminder printed to stderr.
func FormatNotice(status Status) string {
	if !status.Available || status.LatestVersion == "" {
		return ""
	}
	cmd := status.Command
	if cmd == "" {
		cmd = status.ReleaseURL
	}
	return fmt.Sprintf("A newer ketch is available: %s\nUpdate: %s", status.LatestVersion, cmd)
}

func fetchLatestRelease(ctx context.Context) (releaseInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseLatest, nil)
	if err != nil {
		return releaseInfo{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", fmt.Sprintf("ketch/%s (%s/%s)", runtime.Version(), runtime.GOOS, runtime.GOARCH))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return releaseInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return releaseInfo{}, fmt.Errorf("release check failed: %s", resp.Status)
	}
	var payload struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return releaseInfo{}, err
	}
	version := normalizeVersion(payload.TagName)
	if !isVersionCheckable(version) {
		return releaseInfo{}, errors.New("latest release version is not semver")
	}
	if payload.HTMLURL == "" {
		payload.HTMLURL = releaseURL
	}
	return releaseInfo{Version: version, URL: payload.HTMLURL}, nil
}

func statusFromState(state cacheState, currentVersion string, installType InstallType, installCmd string) Status {
	status := Status{
		CheckedAt:     state.LastCheckedAt,
		Available:     compareVersions(currentVersion, state.LatestVersion) < 0,
		LatestVersion: normalizeVersion(state.LatestVersion),
		InstallType:   installType,
		Command:       installCmd,
		ReleaseURL:    state.ReleaseURL,
		Source:        "cache",
	}
	if status.ReleaseURL == "" {
		status.ReleaseURL = releaseURL
	}
	if state.UpdateCommand != "" && os.Getenv("KETCH_UPDATE_COMMAND") == "" {
		status.Command = state.UpdateCommand
	}
	if status.Command == "" {
		status.Command = renderCommand(installType, status.LatestVersion)
	}
	return status
}

func isCacheStale(state cacheState, now time.Time) bool {
	if state.LastCheckedAt.IsZero() {
		return true
	}
	return now.Sub(state.LastCheckedAt) >= checkTTL
}

func inFailureBackoff(state cacheState, now time.Time) bool {
	if state.LastCheckFailedAt.IsZero() {
		return false
	}
	return now.Sub(state.LastCheckFailedAt) < failedRetryTT
}

func loadState() (cacheState, error) {
	path, err := cachePath()
	if err != nil {
		return cacheState{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cacheState{}, err
	}
	var state cacheState
	if err := json.Unmarshal(data, &state); err != nil {
		return cacheState{}, err
	}
	if state.SchemaVersion == 0 {
		state.SchemaVersion = schemaVersion
	}
	return state, nil
}

func saveState(state cacheState) error {
	path, err := cachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func cachePath() (string, error) {
	base, err := ketchDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "update-check.json"), nil
}

func ketchDir() (string, error) {
	if d, err := cacheDirFn(); err == nil {
		return filepath.Join(d, "ketch"), nil
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".ketch"), nil
	}
	return "", fmt.Errorf("cannot determine home or cache directory")
}

func detectInstall() (InstallType, string) {
	if cmd := strings.TrimSpace(os.Getenv("KETCH_UPDATE_COMMAND")); cmd != "" {
		return detectInstallType(), cmd
	}
	typ := detectInstallType()
	return typ, renderCommand(typ, "")
}

func detectInstallType() InstallType {
	if forced := parseInstallType(os.Getenv("KETCH_INSTALL_METHOD")); forced != InstallUnknown {
		return forced
	}
	exe, err := execPathFn()
	if err != nil {
		return InstallUnknown
	}
	if looksLikeHomebrew(exe) {
		return InstallHomebrew
	}
	path := normalizePath(exe)
	if strings.HasSuffix(path, "/go/bin/ketch") || strings.HasSuffix(path, "/go/bin/ketch.exe") || underGoBin(exe) {
		return InstallGo
	}
	if strings.HasSuffix(path, "/ketch") || strings.HasSuffix(path, "/ketch.exe") {
		return InstallManual
	}
	return InstallUnknown
}

func parseInstallType(raw string) InstallType {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(InstallHomebrew):
		return InstallHomebrew
	case string(InstallGo):
		return InstallGo
	case string(InstallManual):
		return InstallManual
	case string(InstallUnknown), "":
		return InstallUnknown
	default:
		return InstallUnknown
	}
}

func renderCommand(installType InstallType, _ string) string {
	if cmd := strings.TrimSpace(os.Getenv("KETCH_UPDATE_COMMAND")); cmd != "" {
		return cmd
	}
	switch installType {
	case InstallHomebrew:
		return "brew upgrade 1broseidon/tap/ketch"
	case InstallGo:
		return "go install github.com/1broseidon/ketch@latest"
	case InstallManual, InstallUnknown:
		return releaseURL
	default:
		return releaseURL
	}
}

func underGoBin(exe string) bool {
	paths := []string{}
	if gobin := strings.TrimSpace(os.Getenv("GOBIN")); gobin != "" {
		paths = append(paths, gobin)
	}
	if gopath := strings.TrimSpace(os.Getenv("GOPATH")); gopath != "" {
		paths = append(paths, filepath.Join(gopath, "bin"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, "go", "bin"))
	}
	for _, candidate := range paths {
		if candidate == "" {
			continue
		}
		if sameDir(exe, candidate) {
			return true
		}
	}
	return false
}

func looksLikeHomebrew(exe string) bool {
	paths := []string{exe}
	if resolved, err := evalSymlinks(exe); err == nil && resolved != "" {
		paths = append(paths, resolved)
	}
	for _, candidate := range paths {
		path := normalizePath(candidate)
		if strings.Contains(path, "/cellar/ketch/") || strings.Contains(path, "/homebrew/cellar/ketch/") {
			return true
		}
	}
	return false
}

func normalizePath(path string) string {
	path = filepath.ToSlash(strings.ToLower(path))
	return strings.ReplaceAll(path, "\\", "/")
}

func sameDir(exe, dir string) bool {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	absExe, err := filepath.Abs(exe)
	if err != nil {
		return false
	}
	return strings.EqualFold(filepath.Dir(absExe), absDir)
}

func isVersionCheckable(v string) bool {
	_, ok := parseVersion(v)
	return ok
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}

func compareVersions(current, latest string) int {
	a, okA := parseVersion(current)
	b, okB := parseVersion(latest)
	if !okA || !okB {
		return 0
	}
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

func parseVersion(v string) ([3]int, bool) {
	v = normalizeVersion(v)
	if v == "vdev" || v == "v(devel)" {
		return [3]int{}, false
	}
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "+-"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var out [3]int
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}
