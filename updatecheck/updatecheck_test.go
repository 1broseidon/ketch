package updatecheck

import (
	"context"
	"testing"
	"time"
)

func TestGetStatusHomebrewCommand(t *testing.T) {
	for _, tt := range []struct {
		name     string
		cached   bool
		override string
		want     string
	}{
		{name: "live", want: "brew upgrade ketch"},
		{name: "cached tap command", cached: true, want: "brew upgrade ketch"},
		{name: "live override", override: "custom upgrade", want: "custom upgrade"},
		{name: "cached override", cached: true, override: "custom upgrade", want: "custom upgrade"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("KETCH_NO_UPDATE_NOTIFIER", "")
			t.Setenv("KETCH_INSTALL_METHOD", "homebrew")
			t.Setenv("KETCH_UPDATE_COMMAND", tt.override)
			cacheDir := t.TempDir()
			oldCacheDir, oldFetch := cacheDirFn, releaseFetch
			t.Cleanup(func() { cacheDirFn, releaseFetch = oldCacheDir, oldFetch })
			cacheDirFn = func() (string, error) { return cacheDir, nil }
			fetches := 0
			releaseFetch = func(context.Context) (releaseInfo, error) {
				fetches++
				return releaseInfo{Version: "0.16.2", URL: releaseURL}, nil
			}
			wantSource, wantFetches := "live", 1
			if tt.cached {
				wantSource, wantFetches = "cache", 0
				if err := saveState(cacheState{
					SchemaVersion: schemaVersion,
					LastCheckedAt: time.Now(),
					LatestVersion: "0.16.2",
					UpdateCommand: "brew upgrade 1broseidon/tap/ketch",
				}); err != nil {
					t.Fatal(err)
				}
			}
			status, err := GetStatus(context.Background(), Options{
				CurrentVersion: "0.16.1",
				AllowNetwork:   true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if status.Command != tt.want || status.InstallType != InstallHomebrew || !status.Available {
				t.Fatalf("status = %+v, want available Homebrew update with command %q", status, tt.want)
			}
			if status.Source != wantSource || fetches != wantFetches {
				t.Fatalf("source = %q, fetches = %d; want %q, %d", status.Source, fetches, wantSource, wantFetches)
			}
		})
	}
}

func TestGetStatusDisabledSkipsCacheAndNetwork(t *testing.T) {
	for _, value := range []string{"1", "true", "yes", "on", " TRUE "} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("KETCH_NO_UPDATE_NOTIFIER", value)
			cacheDir := t.TempDir()
			oldCacheDir, oldFetch := cacheDirFn, releaseFetch
			t.Cleanup(func() { cacheDirFn, releaseFetch = oldCacheDir, oldFetch })
			cacheDirFn = func() (string, error) {
				t.Error("disabled update checker accessed the cache")
				return cacheDir, nil
			}
			releaseFetch = func(context.Context) (releaseInfo, error) {
				t.Error("disabled update checker fetched release information")
				return releaseInfo{Version: "0.16.2", URL: releaseURL}, nil
			}
			status, err := GetStatus(context.Background(), Options{
				CurrentVersion: "0.16.1",
				AllowNetwork:   true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if status.Source != "none" || status.Available || status.LatestVersion != "" {
				t.Fatalf("disabled update status = %+v", status)
			}
		})
	}
}
