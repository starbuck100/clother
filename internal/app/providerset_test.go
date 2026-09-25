package app

import (
	"slices"
	"testing"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/providers"
)

// The routing rules are tested against a stub. This is the other half: that the
// real set, wired to the real catalog and a configuration, answers what the
// rules assume it answers.
func testProviderSet(t *testing.T) providerSet {
	t.Helper()

	catalog, err := providers.Load()
	if err != nil {
		t.Fatalf("providers.Load() error = %v", err)
	}
	return providerSet{
		catalog: catalog,
		config: &config.File{
			Version:           1,
			ProviderOverrides: map[string]config.ProviderOverride{},
			OpenRouterAliases: map[string]string{"kimi": "moonshotai/kimi-k2.6"},
			CustomProviders: map[string]config.CustomProvider{
				"mine": {Name: "mine", BaseURL: "https://example.test", APIKeyEnv: "MINE_API_KEY"},
			},
		},
	}
}

func TestProviderSetIsProvider(t *testing.T) {
	t.Parallel()

	set := testProviderSet(t)

	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"a catalog provider", "zai", true},
		{"the subscription", "native", true},
		{"an openrouter catalog provider", "openrouter", true},
		{"a configured alias as a profile", "or-kimi", true},
		{"a configured custom provider", "mine", true},
		{"an unconfigured alias", "or-nope", false},
		{"an unknown name", "nope", false},
		{"empty", "", false},
		{"a command is not a provider", "help", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := set.IsProvider(tt.in); got != tt.want {
				t.Errorf("IsProvider(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// Only the families whose models are not a fixed list may have a tag read
// straight off the command line, because for the others there is no tag to name.
func TestProviderSetIsModelTagProvider(t *testing.T) {
	t.Parallel()

	set := testProviderSet(t)

	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"openrouter", "openrouter", true},
		{"kilo", "kilo", true},
		{"a custom provider", "mine", true},
		{"a fixed model list", "zai", false},
		{"the subscription", "native", false},
		{"a local backend", "ollama", false},
		{"something that does not resolve", "nope", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := set.IsModelTagProvider(tt.in); got != tt.want {
				t.Errorf("IsModelTagProvider(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// The candidates are what a typo is matched against, so they have to include all
// three kinds of name a user can type: a command, a provider, and an alias.
func TestProviderSetCandidates(t *testing.T) {
	t.Parallel()

	set := testProviderSet(t)
	candidates := set.Candidates()

	for _, want := range []string{"help", "install", "zai", "native", "openrouter", "or-kimi", "mine", "claude"} {
		if !slices.Contains(candidates, want) {
			t.Errorf("Candidates() is missing %q", want)
		}
	}
}

// The set delegates the gateway to the same function the launcher path uses, so
// both entry points resolve `or <alias>` identically.
func TestProviderSetGateway(t *testing.T) {
	t.Parallel()

	set := testProviderSet(t)

	profile, rest, err := set.Gateway("or", []string{"kimi", "--yolo"})
	if err != nil {
		t.Fatalf("Gateway error = %v", err)
	}
	if profile != "or-kimi" {
		t.Errorf("Gateway profile = %q, want %q", profile, "or-kimi")
	}
	if !slices.Equal(rest, []string{"--yolo"}) {
		t.Errorf("Gateway rest = %q, want %q", rest, []string{"--yolo"})
	}

	if _, _, err := set.Gateway("or", nil); err == nil {
		t.Error("Gateway accepted a call with no name")
	}
}
