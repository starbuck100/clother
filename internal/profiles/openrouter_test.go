package profiles

import (
	"strings"
	"testing"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/providers"
)

func openRouterConfig(aliases map[string]string) *config.File {
	if aliases == nil {
		aliases = map[string]string{}
	}
	return &config.File{
		Version:           1,
		ProviderOverrides: map[string]config.ProviderOverride{},
		OpenRouterAliases: aliases,
		CustomProviders:   map[string]config.CustomProvider{},
	}
}

func TestResolveOpenRouter(t *testing.T) {
	t.Parallel()

	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}

	target, err := Resolve("openrouter", catalog, openRouterConfig(nil))
	if err != nil {
		t.Fatalf("Resolve(%q) error = %v", "openrouter", err)
	}

	if target.Family != providers.FamilyOpenRouter {
		t.Errorf("Family = %q, want %q", target.Family, providers.FamilyOpenRouter)
	}
	if target.SecretKey != "OPENROUTER_API_KEY" {
		t.Errorf("SecretKey = %q, want %q", target.SecretKey, "OPENROUTER_API_KEY")
	}
	if target.Model == "" {
		t.Error("Model is empty: the catalog entry needs a default model")
	}
	for _, tier := range []string{"haiku", "sonnet", "opus"} {
		if target.ModelTiers[tier] != target.Model {
			t.Errorf("tier %q = %q, want the default model %q", tier, target.ModelTiers[tier], target.Model)
		}
	}
}

// The catalog entry and the hand-configured alias path must describe the same
// endpoint. A divergence would send one of them to the wrong host, and both go
// through the same secret, so nothing else would catch it.
func TestOpenRouterAgreesWithTheAliasPath(t *testing.T) {
	t.Parallel()

	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}

	cfg := openRouterConfig(map[string]string{"kimi": "moonshotai/kimi-k2.6"})
	catalogTarget, err := Resolve("openrouter", catalog, cfg)
	if err != nil {
		t.Fatalf("Resolve(%q) error = %v", "openrouter", err)
	}
	aliasTarget, err := Resolve("or-kimi", catalog, cfg)
	if err != nil {
		t.Fatalf("Resolve(%q) error = %v", "or-kimi", err)
	}

	if catalogTarget.BaseURL != aliasTarget.BaseURL {
		t.Errorf("base url differs: catalog %q, alias %q", catalogTarget.BaseURL, aliasTarget.BaseURL)
	}
	if catalogTarget.TestURL != aliasTarget.TestURL {
		t.Errorf("test url differs: catalog %q, alias %q", catalogTarget.TestURL, aliasTarget.TestURL)
	}
	if catalogTarget.Family != aliasTarget.Family {
		t.Errorf("family differs: catalog %q, alias %q", catalogTarget.Family, aliasTarget.Family)
	}
	if catalogTarget.SecretKey != aliasTarget.SecretKey {
		t.Errorf("secret key differs: catalog %q, alias %q", catalogTarget.SecretKey, aliasTarget.SecretKey)
	}
}

// A resolved alias still works exactly as it did before the catalog entry
// existed: same path, still reachable, still taking its model from the alias.
func TestOpenRouterAliasesAreUnchanged(t *testing.T) {
	t.Parallel()

	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}

	target, err := Resolve("or-kimi", catalog, openRouterConfig(map[string]string{"kimi": "moonshotai/kimi-k2.6"}))
	if err != nil {
		t.Fatalf("Resolve(%q) error = %v", "or-kimi", err)
	}
	if target.ModelTiers["sonnet"] != "moonshotai/kimi-k2.6" {
		t.Errorf("alias model = %q, want %q", target.ModelTiers["sonnet"], "moonshotai/kimi-k2.6")
	}
	if target.DisplayName != "OpenRouter: kimi" {
		t.Errorf("DisplayName = %q, want %q", target.DisplayName, "OpenRouter: kimi")
	}
}

// An override replaces the default model for every tier, which is how a model
// tag chosen at configure time reaches the launcher.
func TestOpenRouterOverrideReplacesTheDefaultModel(t *testing.T) {
	t.Parallel()

	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}

	cfg := openRouterConfig(nil)
	cfg.ProviderOverrides["openrouter"] = config.ProviderOverride{Model: "moonshotai/kimi-k2.6"}

	target, err := Resolve("openrouter", catalog, cfg)
	if err != nil {
		t.Fatalf("Resolve(%q) error = %v", "openrouter", err)
	}
	if target.Model != "moonshotai/kimi-k2.6" {
		t.Errorf("Model = %q, want the override", target.Model)
	}
	for _, tier := range []string{"haiku", "sonnet", "opus", "small"} {
		if target.ModelTiers[tier] != "moonshotai/kimi-k2.6" {
			t.Errorf("tier %q = %q, want the override", tier, target.ModelTiers[tier])
		}
	}
}

// The catalog data itself, checked where it is cheap to check. OpenRouter routes
// by a vendor-qualified slug, and the batch variants are a different endpoint
// with different timing, so neither belongs in a shortlist offered for
// interactive use.
func TestOpenRouterCatalogEntryModelChoices(t *testing.T) {
	t.Parallel()

	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := catalog.Get("openrouter")
	if !ok {
		t.Fatal("the catalog has no openrouter provider")
	}

	if len(provider.ModelChoices) == 0 {
		t.Fatal("openrouter has no model choices")
	}
	if provider.DefaultModel == "" {
		t.Error("openrouter has no default model")
	}

	choices := map[string]bool{}
	for _, choice := range provider.ModelChoices {
		if !strings.Contains(choice.ID, "/") {
			t.Errorf("model choice %q is not a vendor-qualified slug", choice.ID)
		}
		if strings.Contains(choice.ID, ":") {
			t.Errorf("model choice %q carries a variant suffix, which is not an interactive model", choice.ID)
		}
		if choice.Description == "" {
			t.Errorf("model choice %q has no description", choice.ID)
		}
		choices[choice.ID] = true
	}

	if !choices[provider.DefaultModel] {
		t.Errorf("the default model %q is not among the model choices", provider.DefaultModel)
	}
	if !choices["anthropic/claude-sonnet-5"] {
		t.Error("the shortlist lost the Claude Sonnet entry the default points at")
	}
}
