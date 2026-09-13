package profiles

import (
	"strings"
	"testing"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/providers"
)

// emptyConfig is a config with nothing but the defaults, which is what a fresh
// install has.
func emptyConfig() *config.File {
	return &config.File{
		Version:           1,
		ProviderOverrides: map[string]config.ProviderOverride{},
		OpenRouterAliases: map[string]string{},
		CustomProviders:   map[string]config.CustomProvider{},
	}
}

func loadCatalog(t *testing.T) providers.Catalog {
	t.Helper()

	catalog, err := providers.Load()
	if err != nil {
		t.Fatalf("providers.Load() error = %v", err)
	}
	return catalog
}

func TestActiveWithoutARememberedProvider(t *testing.T) {
	t.Parallel()

	catalog := loadCatalog(t)

	selection, err := Active(catalog, emptyConfig())
	if err != nil {
		t.Fatalf("Active error = %v", err)
	}
	if selection.Target.Profile != DefaultProfile {
		t.Errorf("Target.Profile = %q, want %q", selection.Target.Profile, DefaultProfile)
	}
	if selection.Model != "" {
		t.Errorf("Model = %q, want empty", selection.Model)
	}
	if selection.Note != "" {
		t.Errorf("Note = %q, want empty for an unremarkable default", selection.Note)
	}
}

// A nil config is what the launcher path has before anything is loaded, and it
// must not be a special case that fails.
func TestActiveWithNilConfig(t *testing.T) {
	t.Parallel()

	catalog := loadCatalog(t)

	selection, err := Active(catalog, nil)
	if err != nil {
		t.Fatalf("Active error = %v", err)
	}
	if selection.Target.Profile != DefaultProfile {
		t.Errorf("Target.Profile = %q, want %q", selection.Target.Profile, DefaultProfile)
	}
}

func TestActiveUsesTheRememberedProvider(t *testing.T) {
	t.Parallel()

	catalog := loadCatalog(t)
	cfg := emptyConfig()
	cfg.Active = &config.Active{Profile: "zai"}

	selection, err := Active(catalog, cfg)
	if err != nil {
		t.Fatalf("Active error = %v", err)
	}
	if selection.Target.Profile != "zai" {
		t.Errorf("Target.Profile = %q, want %q", selection.Target.Profile, "zai")
	}
	if selection.Note != "" {
		t.Errorf("Note = %q, want empty when the remembered provider was used", selection.Note)
	}
}

// The model travels separately as --model rather than being folded into the
// target here, so that it reaches Claude Code through the same path an explicit
// --model takes. This is the OpenRouter case, where a free model tag is the
// normal thing to want.
func TestActiveReturnsTheRememberedModel(t *testing.T) {
	t.Parallel()

	catalog := loadCatalog(t)
	cfg := emptyConfig()
	cfg.Active = &config.Active{Profile: "openrouter", Model: "moonshotai/kimi-k2.6"}

	selection, err := Active(catalog, cfg)
	if err != nil {
		t.Fatalf("Active error = %v", err)
	}
	if selection.Target.Profile != "openrouter" {
		t.Errorf("Target.Profile = %q, want %q", selection.Target.Profile, "openrouter")
	}
	if selection.Model != "moonshotai/kimi-k2.6" {
		t.Errorf("Model = %q, want %q", selection.Model, "moonshotai/kimi-k2.6")
	}

	// The invariant: the target is what plain resolution produces, untouched.
	// Whatever the remembered tag is, it is not folded in here.
	plain, err := Resolve("openrouter", catalog, emptyConfig())
	if err != nil {
		t.Fatalf("Resolve error = %v", err)
	}
	if selection.Target.Model != plain.Model {
		t.Errorf("Active changed the target model to %q, want it left at %q", selection.Target.Model, plain.Model)
	}
}

// The reason the fallback exists: state the user cannot repair without a working
// clother must not be able to stop clother from starting.
func TestActiveFallsBackWhenTheRememberedProviderIsGone(t *testing.T) {
	t.Parallel()

	catalog := loadCatalog(t)
	cfg := emptyConfig()
	cfg.Active = &config.Active{
		Profile: "retired-custom-provider",
		// A model tag that belonged to the provider that is now gone. Carrying
		// it over would apply a stranger's model to the subscription.
		Model: "some/old-model",
	}

	selection, err := Active(catalog, cfg)
	if err != nil {
		t.Fatalf("Active error = %v, want a fallback rather than a failure", err)
	}
	if selection.Target.Profile != DefaultProfile {
		t.Errorf("Target.Profile = %q, want the fallback %q", selection.Target.Profile, DefaultProfile)
	}
	if selection.Model != "" {
		t.Errorf("Model = %q, want empty: the tag belonged to the provider that is gone", selection.Model)
	}
	if !strings.Contains(selection.Note, "retired-custom-provider") {
		t.Errorf("Note = %q, want it to name the provider that could not be resolved", selection.Note)
	}
	if !strings.Contains(selection.Note, DefaultProfile) {
		t.Errorf("Note = %q, want it to name the provider that is used instead", selection.Note)
	}
}

func TestActiveTreatsAnEmptyRememberedProfileAsUnset(t *testing.T) {
	t.Parallel()

	catalog := loadCatalog(t)
	cfg := emptyConfig()
	cfg.Active = &config.Active{Profile: "   "}

	selection, err := Active(catalog, cfg)
	if err != nil {
		t.Fatalf("Active error = %v", err)
	}
	if selection.Target.Profile != DefaultProfile {
		t.Errorf("Target.Profile = %q, want %q", selection.Target.Profile, DefaultProfile)
	}
	if selection.Note != "" {
		t.Errorf("Note = %q, want empty: a blank profile is unset, not a substitution", selection.Note)
	}
}

// `claude` is the first thing a new user types, and it is neither a subcommand
// nor a provider id. Without the alias it would be handed to Claude Code as a
// prompt.
func TestAliasesResolveToTheSubscription(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"claude", ResolveAlias("claude"), DefaultProfile},
		{"subscription", ResolveAlias("subscription"), DefaultProfile},
		{"mixed case alias", ResolveAlias("Claude"), DefaultProfile},
		{"surrounding whitespace", ResolveAlias("  claude  "), DefaultProfile},
		{"a real provider is untouched", ResolveAlias("zai"), "zai"},
		{"whitespace is trimmed off a real provider", ResolveAlias("  zai  "), "zai"},
		{"unknown name is left alone", ResolveAlias("nope"), "nope"},
		{"empty stays empty", ResolveAlias(""), ""},
		// Every valid provider name is lowercase by construction, so an
		// uppercase one is not a provider and must be reported as unknown rather
		// than quietly folded into one that exists.
		{"uppercase provider is not folded", ResolveAlias("ZAI"), "ZAI"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.got != tt.want {
				t.Errorf("ResolveAlias = %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func TestActiveResolvesTheClaudeAlias(t *testing.T) {
	t.Parallel()

	catalog := loadCatalog(t)
	cfg := emptyConfig()
	cfg.Active = &config.Active{Profile: "claude"}

	selection, err := Active(catalog, cfg)
	if err != nil {
		t.Fatalf("Active error = %v", err)
	}
	if selection.Target.Profile != DefaultProfile {
		t.Errorf("Target.Profile = %q, want %q", selection.Target.Profile, DefaultProfile)
	}
	if selection.Note != "" {
		t.Errorf("Note = %q, want empty: the alias is a normal spelling, not a substitution", selection.Note)
	}
}

// The provider type is what decides whether Claude Code gets a config overlay
// and whether session sanitisation runs, so the default has to be the strict
// family and nothing else.
func TestDefaultProfileIsTheStrictFamily(t *testing.T) {
	t.Parallel()

	catalog := loadCatalog(t)
	target, err := Resolve(DefaultProfile, catalog, emptyConfig())
	if err != nil {
		t.Fatalf("Resolve(%q) error = %v", DefaultProfile, err)
	}
	if target.Family != providers.FamilyClaudeStrict {
		t.Errorf("%s resolves to family %q, want %q", DefaultProfile, target.Family, providers.FamilyClaudeStrict)
	}
}
