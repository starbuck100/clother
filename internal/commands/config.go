package commands

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/jolehuit/clother/internal/budget"
	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/launchers"
	"github.com/jolehuit/clother/internal/openrouter"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
	"github.com/jolehuit/clother/internal/runtime"
)

func runConfig(ctx context.Context, c Context, args []string) (int, error) {
	providerID := ""
	if len(args) > 0 {
		providerID = args[0]
	} else {
		var err error
		providerID, err = chooseProvider(c)
		if err != nil || providerID == "" {
			return 0, err
		}
	}

	switch providerID {
	case "budget":
		if len(args) == 1 {
			b := budget.Defaults(c.Config.Budget)
			fmt.Fprintf(c.Output.Stdout, "Paid fallback limits: $%.2f per UTC day, $%.2f per session (local reservations/estimates).\n", b.Daily, b.Session)
			return 0, nil
		}
		if len(args) != 3 {
			return 2, fmt.Errorf("usage: clother config budget <daily-USD> <session-USD>; zero blocks paid fallback")
		}
		daily, e1 := strconv.ParseFloat(args[1], 64)
		session, e2 := strconv.ParseFloat(args[2], 64)
		if e1 != nil || e2 != nil || !budget.Valid(daily) || !budget.Valid(session) {
			return 2, fmt.Errorf("budget must be finite, nonnegative and <= 1000000 USD")
		}
		b := budget.Defaults(c.Config.Budget)
		b.Daily = daily
		b.Session = session
		c.Config.Budget = &b
		if e := config.SaveConfig(c.Paths.ConfigFile, c.Config); e != nil {
			return 1, e
		}
		fmt.Fprintln(c.Output.Stdout, "Budget saved; applies to new sessions. Use /clother:free to block paid requests in this session.")
		return 0, nil
	case "fallback":
		if len(args) != 2 || (args[1] != "paid" && args[1] != "free") {
			return 2, fmt.Errorf("usage: clother config fallback free|paid (paid enables configured API keys after free routes)")
		}
		c.Config.FallbackPaid = args[1] == "paid"
		if err := config.SaveConfig(c.Paths.ConfigFile, c.Config); err != nil {
			return 1, err
		}
		fmt.Fprintf(c.Output.Stdout, "Automatic fallback: free providers first; configured paid keys enabled: %t\n", c.Config.FallbackPaid)
		return 0, nil
	case "kilo":
		return configKilo(ctx, c)
	case "openrouter":
		return configOpenRouter(ctx, c)
	case "custom":
		return configCustom(c)
	default:
		if provider, ok := c.Catalog.Get(providerID); ok {
			return configBuiltin(c, provider)
		}
		return 1, fmt.Errorf("unknown provider %q", providerID)
	}
}

func chooseProvider(c Context) (string, error) {
	index := 1
	choices := map[int]string{}
	c.Output.Header("Clother Configuration")
	for _, category := range c.Catalog.Categories() {
		fmt.Fprintln(c.Output.Stdout, category)
		for _, provider := range c.Catalog.ProvidersByCategory(category) {
			fmt.Fprintf(c.Output.Stdout, "  %2d. %-14s %s\n", index, provider.ID, provider.Description)
			choices[index] = provider.ID
			index++
		}
	}
	// OpenRouter used to be appended here by hand, because it was not in the
	// catalog. It is now, so listing it again would offer it twice.
	fmt.Fprintf(c.Output.Stdout, "  %2d. %-14s %s\n", index, "custom", "Anthropic-compatible endpoint")
	choices[index] = "custom"

	answer, err := c.Prompt.Prompt("Choose provider number", "")
	if err != nil {
		return "", err
	}
	for number, providerID := range choices {
		if fmt.Sprint(number) == answer {
			return providerID, nil
		}
	}
	return "", fmt.Errorf("invalid choice %q", answer)
}

func configBuiltin(c Context, provider providers.Provider) (int, error) {
	if provider.AuthMode == providers.AuthSecret {
		current := c.Secrets[provider.KeyVar]
		if current != "" {
			fmt.Fprintf(c.Output.Stdout, "Current key: %s\n", config.MaskSecret(current))
		}
		label := "API key"
		if current != "" {
			label = "API key (empty to keep current)"
		}
		value, err := c.Prompt.PromptSecret(label)
		if err != nil {
			return 1, err
		}
		if strings.TrimSpace(value) != "" {
			c.Secrets[provider.KeyVar] = value
		}
	}

	override := c.Config.ProviderOverrides[provider.ID]

	if provider.DefaultModel != "" {
		fmt.Fprintln(c.Output.Stdout, "Choose model:")
		for idx, choice := range provider.ModelChoices {
			fmt.Fprintf(c.Output.Stdout, "  %d. %-24s %s\n", idx+1, choice.ID, choice.Description)
		}
		defaultValue := provider.DefaultModel
		if override.Model != "" {
			defaultValue = override.Model
		}
		answer, err := c.Prompt.Prompt("Model", defaultValue)
		if err != nil {
			return 1, err
		}
		answer = resolveModelChoice(answer, provider.ModelChoices)
		if answer != "" && answer != provider.DefaultModel {
			override.Model = answer
		} else {
			override.Model = ""
		}
	}

	// Local backends may run on another machine (e.g. LM Studio on a LAN
	// host), so let the user point the launcher at a remote base URL.
	if provider.Family == providers.FamilyLocal {
		defaultURL := provider.BaseURL
		if override.BaseURL != "" {
			defaultURL = override.BaseURL
		}
		answer, err := c.Prompt.Prompt("Base URL", defaultURL)
		if err != nil {
			return 1, err
		}
		answer = strings.TrimSpace(answer)
		if answer != "" && answer != provider.BaseURL {
			if !strings.HasPrefix(answer, "http://") && !strings.HasPrefix(answer, "https://") {
				return 1, fmt.Errorf("invalid base URL %q (must start with http:// or https://)", answer)
			}
			override.BaseURL = strings.TrimRight(answer, "/")
		} else {
			override.BaseURL = ""
		}
	}

	if override == (config.ProviderOverride{}) {
		delete(c.Config.ProviderOverrides, provider.ID)
	} else {
		c.Config.ProviderOverrides[provider.ID] = override
	}
	return persistConfig(c)
}

func configOpenRouter(ctx context.Context, c Context) (int, error) {
	current := c.Secrets["OPENROUTER_API_KEY"]
	if current != "" {
		fmt.Fprintf(c.Output.Stdout, "Current key: %s\n", config.MaskSecret(current))
	}
	value, err := c.Prompt.PromptSecret("OpenRouter API key (empty to keep current)")
	if err != nil {
		return 1, err
	}
	if value = strings.TrimSpace(value); value != "" {
		c.Secrets["OPENROUTER_API_KEY"] = value
	}
	if c.Secrets["OPENROUTER_API_KEY"] == "" {
		return 1, fmt.Errorf("OpenRouter API key is required")
	}
	provider, ok := c.Catalog.Get("openrouter")
	if !ok {
		return 1, fmt.Errorf("OpenRouter provider missing from catalog")
	}
	override := c.Config.ProviderOverrides["openrouter"]
	defaultModel := provider.DefaultModel
	if override.Model != "" {
		defaultModel = override.Model
	}
	baseURL := provider.BaseURL
	if override.BaseURL != "" {
		baseURL = override.BaseURL
	}
	fmt.Fprintln(c.Output.Stdout, "Fetching current OpenRouter models...")
	catalog, err := openrouter.Load(ctx, baseURL, c.Paths.CacheDir, true)
	if err != nil {
		return 1, err
	}
	choices := []providers.ModelChoice{}
	fmt.Fprintln(c.Output.Stdout, "Currently free models with text and tool support:")
	for idx, choice := range catalog.FreeModels() {
		output := fmt.Sprint(choice.TopProvider.MaxCompletionTokens)
		if choice.TopProvider.MaxCompletionTokens <= 0 {
			output = "unspecified (conservative limit at launch)"
		}
		fmt.Fprintf(c.Output.Stdout, "  %d. %s | context: %d | max output: %s\n", idx+1, choice.ID, choice.ContextLimit(), output)
		choices = append(choices, providers.ModelChoice{ID: choice.ID})
	}
	if len(choices) == 0 {
		fmt.Fprintln(c.Output.Stdout, "  No compatible free models currently advertised.")
	}
	fmt.Fprintln(c.Output.Stdout, "Choose a number or enter any catalog model ID (paid models allowed). Free models still have provider rate limits.")
	model, err := c.Prompt.Prompt("Default model", defaultModel)
	if err != nil {
		return 1, err
	}
	model = resolveModelChoice(model, choices)
	if err := validateOpenRouterSelection(catalog, model); err != nil {
		return 1, err
	}
	override.Model = model
	c.Config.ProviderOverrides["openrouter"] = override
	fmt.Fprintln(c.Output.Stdout, "Optional model aliases for clother-or <alias>:")
	for {
		model, err := c.Prompt.Prompt("Model number or ID (empty to stop)", "")
		if err != nil {
			return 1, err
		}
		if strings.TrimSpace(model) == "" {
			break
		}
		model = resolveModelChoice(model, choices)
		if err := validateOpenRouterSelection(catalog, model); err != nil {
			return 1, err
		}
		name, err := c.Prompt.Prompt("Alias", defaultAliasName(model))
		if err != nil {
			return 1, err
		}
		if !profiles.IsProviderName(name) {
			return 1, fmt.Errorf("invalid alias %q (use lowercase letters, digits, \"-\" or \"_\")", name)
		}
		c.Config.OpenRouterAliases[name] = model
	}
	return persistConfig(c)
}

func configCustom(c Context) (int, error) {
	name, err := c.Prompt.Prompt("Provider name", "")
	if err != nil {
		return 1, err
	}
	if !profiles.IsProviderName(name) {
		return 1, fmt.Errorf("invalid provider name %q", name)
	}

	existing := c.Config.CustomProviders[name]

	urlLabel := "Base URL"
	if existing.BaseURL != "" {
		fmt.Fprintf(c.Output.Stdout, "Current URL: %s\n", existing.BaseURL)
		urlLabel = "Base URL (empty to keep current)"
	}
	baseURL, err := c.Prompt.Prompt(urlLabel, "")
	if err != nil {
		return 1, err
	}
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = existing.BaseURL
	}
	if baseURL == "" {
		return 1, fmt.Errorf("base URL is required")
	}

	modelLabel := "Default model (optional)"
	if existing.DefaultModel != "" {
		modelLabel = fmt.Sprintf("Default model (empty to keep %q)", existing.DefaultModel)
	}
	defaultModel, err := c.Prompt.Prompt(modelLabel, "")
	if err != nil {
		return 1, err
	}
	defaultModel = strings.TrimSpace(defaultModel)
	if defaultModel == "" {
		defaultModel = existing.DefaultModel
	}

	keyVar := strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_API_KEY"
	current := c.Secrets[keyVar]
	keyLabel := "API key"
	if current != "" {
		fmt.Fprintf(c.Output.Stdout, "Current key: %s\n", config.MaskSecret(current))
		keyLabel = "API key (empty to keep current)"
	}
	apiKey, err := c.Prompt.PromptSecret(keyLabel)
	if err != nil {
		return 1, err
	}
	if strings.TrimSpace(apiKey) != "" {
		c.Secrets[keyVar] = apiKey
	}

	c.Config.CustomProviders[name] = config.CustomProvider{
		Name:         name,
		DisplayName:  name,
		BaseURL:      baseURL,
		APIKeyEnv:    keyVar,
		DefaultModel: defaultModel,
	}
	return persistConfig(c)
}

func persistConfig(c Context) (int, error) {
	config.NormalizeLegacySecrets(c.Secrets, c.Catalog)
	if err := config.SaveConfig(c.Paths.ConfigFile, c.Config); err != nil {
		return 1, err
	}
	if err := config.SaveSecrets(c.Paths.SecretsFile, c.Secrets); err != nil {
		return 1, err
	}
	execPath, execErr := os.Executable()
	if execErr != nil {
		return 1, execErr
	}
	if err := launchers.Sync(execPath, c.Paths, c.Catalog, c.Config, launchers.SyncOptions{
		SkipCopy:          runtime.IsHomebrew(),
		InstallClaudeShim: !c.Options.NoShim,
	}); err != nil {
		return 1, err
	}
	c.Output.Success("configuration saved")
	return 0, nil
}

func defaultAliasName(model string) string {
	model = strings.ToLower(model)
	if slash := strings.LastIndex(model, "/"); slash >= 0 {
		model = model[slash+1:]
	}
	// Model IDs may carry characters that are invalid in an alias (used as
	// launcher name), e.g. the ":free"/":exacto" variant suffixes. Map anything
	// outside the alias charset to "-" so the suggested default always passes
	// profiles.IsProviderName.
	var b strings.Builder
	for _, r := range model {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	name := b.String()
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	return strings.Trim(name, "-")
}

func resolveModelChoice(answer string, choices []providers.ModelChoice) string {
	answer = strings.TrimSpace(answer)
	if idx, err := strconv.Atoi(answer); err == nil {
		if idx >= 1 && idx <= len(choices) {
			return choices[idx-1].ID
		}
	}
	return answer
}

func validateOpenRouterSelection(catalog openrouter.Catalog, id string) error {
	if !profiles.IsModelTag(id) {
		return fmt.Errorf("invalid OpenRouter model %q (use vendor/model)", id)
	}
	model, err := catalog.Find(id)
	if err != nil {
		return err
	}
	return model.Validate()
}
