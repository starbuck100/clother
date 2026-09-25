package commands

import (
	"context"
	"fmt"
	"github.com/jolehuit/clother/internal/kilo"
	"github.com/jolehuit/clother/internal/providers"
	"strings"
)

func configKilo(ctx context.Context, c Context) (int, error) {
	fmt.Fprintln(c.Output.Stdout, "Kilo Gateway: no Kilo Code installation required. Free models can be used anonymously.")
	value, err := c.Prompt.PromptSecret("Optional Kilo API key (empty: keep current or anonymous; -: clear)")
	if err != nil {
		return 1, err
	}
	key := c.Secrets["KILO_API_KEY"]
	if value = strings.TrimSpace(value); value == "-" {
		key = ""
	} else if value != "" {
		key = value
	}
	provider, ok := c.Catalog.Get("kilo")
	if !ok {
		return 1, fmt.Errorf("Kilo provider missing")
	}
	override := c.Config.ProviderOverrides["kilo"]
	base := provider.BaseURL
	if override.BaseURL != "" {
		base = override.BaseURL
	}
	fmt.Fprintln(c.Output.Stdout, "Fetching current Kilo models...")
	catalog, err := kilo.Load(ctx, base, c.Paths.CacheDir, true)
	if err != nil {
		return 1, err
	}
	choices := []providers.ModelChoice{}
	fmt.Fprintln(c.Output.Stdout, "Currently free models with text and tool support:")
	for index, model := range catalog.FreeModels() {
		privacy := ""
		if model.MayTrainOnPrompts {
			privacy = " | provider may train on prompts"
		}
		output := fmt.Sprint(model.TopProvider.MaxCompletionTokens)
		if model.TopProvider.MaxCompletionTokens <= 0 {
			output = "unspecified (conservative cap)"
		}
		fmt.Fprintf(c.Output.Stdout, "  %d. %s | context: %d | max output: %s%s\n", index+1, model.ID, model.ContextLimit(), output, privacy)
		choices = append(choices, providers.ModelChoice{ID: model.ID})
	}
	if len(choices) == 0 {
		fmt.Fprintln(c.Output.Stdout, "No compatible free models currently advertised.")
	}
	fmt.Fprintln(c.Output.Stdout, "Choose a number or model ID. Free endpoints have rate limits; paid models require a Kilo key.")
	defaultModel := provider.DefaultModel
	if override.Model != "" {
		defaultModel = override.Model
	}
	answer, err := c.Prompt.Prompt("Default model", defaultModel)
	if err != nil {
		return 1, err
	}
	model, err := catalog.Find(resolveModelChoice(answer, choices))
	if err != nil {
		return 1, err
	}
	if err = model.Validate(); err != nil {
		return 1, err
	}
	if !model.Free() && key == "" {
		return 1, fmt.Errorf("a Kilo API key is required for this model")
	}
	override.Model = model.ID
	c.Config.ProviderOverrides["kilo"] = override
	if key == "" {
		delete(c.Secrets, "KILO_API_KEY")
	} else {
		c.Secrets["KILO_API_KEY"] = key
	}
	return persistConfig(c)
}
