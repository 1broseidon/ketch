package docs

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	config "github.com/1broseidon/ketch/internal/configbase"
)

type libraryFixture struct{}

func (libraryFixture) Search(context.Context, string, int) ([]Result, error) { return nil, nil }
func (libraryFixture) ResolveLibrary(context.Context, string, int) ([]LibraryMatch, error) {
	return nil, nil
}
func (libraryFixture) GetDocs(context.Context, string, string, int) ([]Result, error) {
	return nil, nil
}

func TestRegistryDiscoversOptionalLibraryInterface(t *testing.T) {
	previous := providers
	before := LibraryBackends()
	providers = append(Providers(), Provider{
		ID: "libraryfixture", Name: "Library Fixture",
		Usable: func(*config.Config) bool { return true },
		New:    func(*config.Config) (Searcher, error) { return libraryFixture{}, nil },
	})
	t.Cleanup(func() { providers = previous })
	if !slices.Equal(LibraryBackends(), append(before, "libraryfixture")) {
		t.Fatalf("library capability discovery = %v", LibraryBackends())
	}
	if ResolveBackend("libraryfixture") != "libraryfixture" || ResolveBackend("local") != "context7" {
		t.Fatal("library selection or legacy resolve fallback changed")
	}
	client, err := NewFromConfig(&config.Config{}, "libraryfixture")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := client.(LibraryResolver); !ok {
		t.Fatal("registry lost the optional library interface")
	}
}

func TestRegistryKeepsUnknownAndUnavailableDistinct(t *testing.T) {
	for _, backend := range []string{"local", "context7", "unknown"} {
		_, err := NewFromConfig(&config.Config{}, backend)
		if err == nil || errors.Is(err, ErrUnknownBackend) != (backend == "unknown") {
			t.Fatalf("%s error taxonomy = %v", backend, err)
		}
		if backend == "local" && !strings.Contains(err.Error(), "not yet implemented") {
			t.Fatalf("local rejection changed: %v", err)
		}
	}
}
