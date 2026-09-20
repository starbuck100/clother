package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The two documents the retry tests swap in. B carries everything A has plus an
// alias written by a concurrent writer, so a write based on A can be told apart
// from a write based on B by whether the alias survived.
const (
	activeDocA = `{
  "version": 1,
  "provider_overrides": {
    "zai": {
      "model": "glm-5.2"
    }
  }
}
`
	activeDocB = `{
  "version": 1,
  "provider_overrides": {
    "zai": {
      "model": "glm-5.2"
    }
  },
  "openrouter_aliases": {
    "concurrent": "moonshotai/kimi-k2.6"
  }
}
`
)

// stubReadActiveFile replaces the reader SetActive uses, so a test can make the
// document change between the read that builds the write and the read that
// checks it. After the list is exhausted the last entry repeats, which is what a
// caller wants for "this is the content from now on".
func stubReadActiveFile(t *testing.T, contents ...[]byte) {
	t.Helper()

	calls := 0
	readActiveFile = func(string) ([]byte, error) {
		index := calls
		calls++
		if index >= len(contents) {
			index = len(contents) - 1
		}
		return contents[index], nil
	}
	t.Cleanup(func() { readActiveFile = os.ReadFile })
}

func TestSetActiveCreatesTheDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	if err := SetActive(path, "zai", ""); err != nil {
		t.Fatalf("SetActive error = %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig error = %v", err)
	}
	if cfg.Active == nil {
		t.Fatal("the document has no active provider after SetActive")
	}
	if cfg.Active.Profile != "zai" {
		t.Errorf("Active.Profile = %q, want %q", cfg.Active.Profile, "zai")
	}
	if cfg.Active.Model != "" {
		t.Errorf("Active.Model = %q, want empty", cfg.Active.Model)
	}
	// A document written from nothing still has to be a valid one, which means
	// the version LoadConfig defaults to and not zero.
	if cfg.Version != 1 {
		t.Errorf("Version = %d, want 1", cfg.Version)
	}
}

// The whole point of taking a path instead of a *File: everything the caller
// does not own has to survive.
func TestSetActivePreservesOtherFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := `{
  "version": 1,
  "provider_overrides": {
    "zai": {
      "model": "glm-5.1"
    }
  },
  "openrouter_aliases": {
    "kimi": "moonshotai/kimi-k2.6"
  },
  "custom_providers": {
    "mine": {
      "name": "mine",
      "display_name": "Mine",
      "base_url": "https://example.test",
      "api_key_env": "MINE_API_KEY"
    }
  }
}
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SetActive(path, "openrouter", "z-ai/glm-5.3"); err != nil {
		t.Fatalf("SetActive error = %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig error = %v", err)
	}
	if got := cfg.ProviderOverrides["zai"].Model; got != "glm-5.1" {
		t.Errorf("provider override lost: zai model = %q, want %q", got, "glm-5.1")
	}
	if got := cfg.OpenRouterAliases["kimi"]; got != "moonshotai/kimi-k2.6" {
		t.Errorf("openrouter alias lost: kimi = %q", got)
	}
	if got := cfg.CustomProviders["mine"].BaseURL; got != "https://example.test" {
		t.Errorf("custom provider lost: base url = %q", got)
	}
	if cfg.Active == nil || cfg.Active.Profile != "openrouter" || cfg.Active.Model != "z-ai/glm-5.3" {
		t.Errorf("Active = %+v, want openrouter with z-ai/glm-5.3", cfg.Active)
	}
}

// Switching away from a provider that had a model tag must not leave the tag
// behind: a model only means something for the provider it was chosen for.
func TestSetActiveClearsTheModelWhenNoneIsGiven(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	if err := SetActive(path, "openrouter", "moonshotai/kimi-k2.6"); err != nil {
		t.Fatalf("SetActive error = %v", err)
	}
	if err := SetActive(path, "zai", ""); err != nil {
		t.Fatalf("SetActive error = %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig error = %v", err)
	}
	if cfg.Active.Profile != "zai" || cfg.Active.Model != "" {
		t.Errorf("Active = %+v, want zai with no model", cfg.Active)
	}

	// The tag is also gone from the document, not just from the struct.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "kimi-k2.6") {
		t.Errorf("the discarded model tag is still in the document:\n%s", raw)
	}
}

func TestSetActiveRejectsACorruptDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	corrupt := []byte("{\n  \"version\": 1,\n")
	if err := os.WriteFile(path, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SetActive(path, "zai", ""); err == nil {
		t.Fatal("SetActive accepted a corrupt document")
	}

	// A refused write must leave the file exactly as it was, so a user can go
	// and fix it rather than losing it to a half-applied write.
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(corrupt) {
		t.Errorf("the corrupt document was modified:\n got %q\nwant %q", after, corrupt)
	}
}

// A writer that changed the document while this one was building its write: the
// stale copy is dropped, the document is read again, and the result carries both
// the concurrent change and this one.
func TestSetActiveRetriesAndMerges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	stubReadActiveFile(t, []byte(activeDocA), []byte(activeDocB))

	if err := SetActive(path, "openrouter", "moonshotai/kimi-k2.6"); err != nil {
		t.Fatalf("SetActive error = %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig error = %v", err)
	}
	// This is the assertion that distinguishes a re-read from a stale write: the
	// alias only exists in the document that was read the second time.
	if got := cfg.OpenRouterAliases["concurrent"]; got != "moonshotai/kimi-k2.6" {
		t.Errorf("the concurrent change was clobbered: alias = %q", got)
	}
	if got := cfg.ProviderOverrides["zai"].Model; got != "glm-5.2" {
		t.Errorf("provider override lost: zai model = %q, want %q", got, "glm-5.2")
	}
	if cfg.Active == nil || cfg.Active.Profile != "openrouter" {
		t.Errorf("Active = %+v, want openrouter", cfg.Active)
	}
}

// A document that keeps changing is reported rather than fought over, and the
// failed call must not have written anything.
func TestSetActiveGivesUpWhenTheDocumentKeepsChanging(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	// Three attempts, two reads each: every attempt sees the document change
	// between its read and its check.
	stubReadActiveFile(t,
		[]byte(activeDocA), []byte(activeDocB),
		[]byte(activeDocA), []byte(activeDocB),
		[]byte(activeDocA), []byte(activeDocB),
	)

	err := SetActive(path, "zai", "")
	if err == nil {
		t.Fatal("SetActive reported success while the document kept changing")
	}

	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("a failed SetActive wrote to %s anyway", path)
	}
}
