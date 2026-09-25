package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/jolehuit/clother/internal/openrouter"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
)

var openRouterLimitKeys = []string{
	"CLAUDE_CODE_MAX_CONTEXT_TOKENS",
	"CLAUDE_CODE_MAX_OUTPUT_TOKENS",
	"MAX_THINKING_TOKENS",
	"CLAUDE_CODE_DISABLE_THINKING",
	"CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING",
}

func PrepareOpenRouterProxy(ctx context.Context, target profiles.Target, args, env []string) ([]string, func(), error) {
	envMap := envSliceToMap(env)
	model := ModelOverride(args)
	if model == "" {
		model = effectiveSessionModel(target, envMap)
	}
	if target.Family != providers.FamilyOpenRouter || !openrouter.NeedsSchemaCompatibility(model) {
		return env, func() {}, nil
	}
	endpoint, token, cleanup, err := openrouter.StartSchemaProxy(ctx, target.BaseURL, envMap["ANTHROPIC_AUTH_TOKEN"])
	if err != nil {
		return nil, nil, err
	}
	envMap["ANTHROPIC_BASE_URL"] = endpoint
	envMap["ANTHROPIC_AUTH_TOKEN"] = token
	return flattenEnv(envMap), cleanup, nil
}

func PrepareOpenRouterEnv(ctx context.Context, cacheDir string, target profiles.Target, args, env []string) ([]string, error) {
	if target.Family != providers.FamilyOpenRouter {
		return env, nil
	}
	envMap := envSliceToMap(env)
	id := ModelOverride(args)
	if id == "" {
		id = effectiveSessionModel(target, envMap)
	}
	catalog, err := openrouter.Load(ctx, target.BaseURL, cacheDir, false)
	if err != nil {
		return nil, err
	}
	model, err := catalog.Find(id)
	if err != nil {
		return nil, err
	}
	if err := model.Validate(); err != nil {
		return nil, err
	}
	// Settings env overrides the inherited environment in Claude Code. Honor
	// smaller user limits there, then put bounded values into the overlay too.
	source := envMap["CLAUDE_CONFIG_DIR"]
	if source == "" {
		source = filepath.Join(userHomeDir(), ".claude")
	}
	if data, err := os.ReadFile(filepath.Join(source, "settings.json")); err == nil {
		var settings struct {
			Env map[string]string `json:"env"`
		}
		if err := json.Unmarshal(data, &settings); err != nil {
			return nil, fmt.Errorf("decode Claude settings: %w", err)
		}
		for _, key := range openRouterLimitKeys {
			if value, ok := settings.Env[key]; ok {
				envMap[key] = value
			}
		}
	}
	applyOpenRouterLimits(model, envMap)
	return flattenEnv(envMap), nil
}

func applyOpenRouterLimits(model openrouter.Model, env map[string]string) {
	contextLimit := boundedPositive(env["CLAUDE_CODE_MAX_CONTEXT_TOKENS"], model.ContextLimit())
	// Reserve most of the window for Claude's system prompt, tools and history.
	// Output and thinking count against that same context window.
	output := min(32000, max(1, contextLimit/4))
	if model.TopProvider.MaxCompletionTokens > 0 {
		output = min(output, model.TopProvider.MaxCompletionTokens)
	} else {
		// Routers and some providers do not advertise an output ceiling.
		output = min(output, 4096)
	}
	output = boundedPositive(env["CLAUDE_CODE_MAX_OUTPUT_TOKENS"], output)
	thinking := 0
	if model.Supports("reasoning") && env["CLAUDE_CODE_DISABLE_THINKING"] != "1" {
		thinking = min(8192, output/2)
		if requested, err := strconv.Atoi(env["MAX_THINKING_TOKENS"]); err == nil && requested >= 0 {
			thinking = min(thinking, requested)
		}
		if thinking < 1024 {
			thinking = 0
		}
	}
	env["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] = strconv.Itoa(contextLimit)
	env["CLAUDE_CODE_MAX_OUTPUT_TOKENS"] = strconv.Itoa(output)
	env["MAX_THINKING_TOKENS"] = strconv.Itoa(thinking)
	env["CLAUDE_CODE_DISABLE_ADAPTIVE_THINKING"] = "1"
	if thinking == 0 {
		env["CLAUDE_CODE_DISABLE_THINKING"] = "1"
	} else {
		env["CLAUDE_CODE_DISABLE_THINKING"] = "0"
	}
}

func boundedPositive(value string, ceiling int) int {
	if requested, err := strconv.Atoi(value); err == nil && requested > 0 {
		return min(requested, ceiling)
	}
	return ceiling
}
