package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jolehuit/clother/internal/cli"
	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/providers"
	"github.com/jolehuit/clother/internal/ui"
)

// testSessionContext is a Context whose configuration lives in a temp directory,
// with its output captured so a test can read what the helper said.
func testSessionContext(t *testing.T) (Context, *bytes.Buffer) {
	t.Helper()

	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, ".cache"))
	t.Setenv("CLOTHER_BIN", filepath.Join(root, "bin"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CLOTHER_PROFILE", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")

	paths, err := config.Detect("")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}

	out := &bytes.Buffer{}
	return Context{
		Paths: paths,
		Config: &config.File{
			Version:           1,
			ProviderOverrides: map[string]config.ProviderOverride{},
			OpenRouterAliases: map[string]string{"kimi": "moonshotai/kimi-k2.6"},
			CustomProviders:   map[string]config.CustomProvider{},
		},
		Secrets: config.Secrets{},
		Catalog: catalog,
		Output:  &ui.Output{Stdout: out, Stderr: out, Format: ui.FormatHuman},
		Prompt:  ui.NewPrompter(strings.NewReader(""), out),
	}, out
}

// configNotWritten asserts that a rejected call left nothing behind. The helper
// is the only thing that ever writes the remembered provider, so a file here
// means a refusal did not refuse.
func configNotWritten(t *testing.T, ctx Context) {
	t.Helper()

	if _, err := os.Stat(ctx.Paths.ConfigFile); !os.IsNotExist(err) {
		t.Errorf("the configuration was written at %s despite the call failing", ctx.Paths.ConfigFile)
	}
}

// The routing layer decides that a token is a subcommand; this package decides
// what that subcommand does. A name in one register and not the other is either
// unreachable or undefined, so the two have to be the same set.
func TestCommandsMatchTheRoutingRegister(t *testing.T) {
	t.Parallel()

	fromRouting := cli.CommandNames()
	fromDispatch := CommandNames()
	slices.Sort(fromRouting)

	if !slices.Equal(fromRouting, fromDispatch) {
		t.Errorf("the two command registers disagree:\n routing:  %q\n dispatch: %q", fromRouting, fromDispatch)
	}
}

func TestSessionProviderStoresTheChoice(t *testing.T) {
	ctx, out := testSessionContext(t)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "abc-123")

	code, err := runSession(context.Background(), ctx, []string{"provider", "zai"})
	if err != nil {
		t.Fatalf("runSession error = %v", err)
	}
	if code != 0 {
		t.Fatalf("runSession code = %d, want 0", code)
	}

	cfg, err := config.LoadConfig(ctx.Paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Active == nil || cfg.Active.Profile != "zai" {
		t.Fatalf("Active = %+v, want zai", cfg.Active)
	}

	// The whole point of the helper: the session cannot switch, so it hands back
	// the line that does.
	printed := out.String()
	if !strings.Contains(printed, "clother zai --resume abc-123") {
		t.Errorf("output does not contain the resume line:\n%s", printed)
	}
}

func TestSessionProviderStoresAModelTag(t *testing.T) {
	ctx, out := testSessionContext(t)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "abc-123")

	code, err := runSession(context.Background(), ctx, []string{"provider", "openrouter", "moonshotai/kimi-k2.6"})
	if err != nil {
		t.Fatalf("runSession error = %v", err)
	}
	if code != 0 {
		t.Fatalf("runSession code = %d, want 0", code)
	}

	cfg, err := config.LoadConfig(ctx.Paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Active == nil || cfg.Active.Profile != "openrouter" || cfg.Active.Model != "moonshotai/kimi-k2.6" {
		t.Fatalf("Active = %+v, want openrouter with the model tag", cfg.Active)
	}
	if !strings.Contains(out.String(), "--model moonshotai/kimi-k2.6") {
		t.Errorf("output does not carry the model tag:\n%s", out.String())
	}
}

// The alias is a normal spelling, so storing `claude` has to store the provider
// it means rather than a name nothing resolves.
func TestSessionProviderResolvesTheAlias(t *testing.T) {
	ctx, _ := testSessionContext(t)

	code, err := runSession(context.Background(), ctx, []string{"provider", "claude"})
	if err != nil {
		t.Fatalf("runSession error = %v", err)
	}
	if code != 0 {
		t.Fatalf("runSession code = %d, want 0", code)
	}

	cfg, err := config.LoadConfig(ctx.Paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Active == nil || cfg.Active.Profile != "native" {
		t.Fatalf("Active = %+v, want the alias resolved to native", cfg.Active)
	}
}

func TestSessionProviderWithoutArgumentsOnlyReports(t *testing.T) {
	ctx, out := testSessionContext(t)

	code, err := runSession(context.Background(), ctx, []string{"provider"})
	if err != nil {
		t.Fatalf("runSession error = %v", err)
	}
	if code != 0 {
		t.Fatalf("runSession code = %d, want 0", code)
	}
	// Nothing was chosen yet, so the report names the default the next bare
	// `clother` would launch.
	if !strings.Contains(out.String(), "native") {
		t.Errorf("output does not name the default provider:\n%s", out.String())
	}
	configNotWritten(t, ctx)
}

// The reason this input validation exists at all: the provider name reaches a
// shell in a generated file's instructions and lands in the configuration, so a
// name that is not a name has to be refused here.
func TestSessionProviderRejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"a shell metacharacter in the provider name", []string{"provider", "zai; rm -rf /"}},
		{"a path in the provider name", []string{"provider", "zai/other"}},
		{"a leading dash", []string{"provider", "-zai"}},
		{"an unknown provider", []string{"provider", "not-a-provider"}},
		{"a model tag without a vendor", []string{"provider", "openrouter", "kimi-k2.6"}},
		{"a model tag with a shell metacharacter", []string{"provider", "openrouter", "vendor/model; rm -rf /"}},
		{"no action", nil},
		{"an unknown action", []string{"providerx"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := testSessionContext(t)

			code, err := runSession(context.Background(), ctx, tt.args)
			if err == nil {
				t.Fatal("runSession accepted invalid input")
			}
			if code != 2 {
				t.Errorf("runSession code = %d, want 2 for invalid input", code)
			}
			configNotWritten(t, ctx)
		})
	}
}

// Without the environment Claude Code exports, the honest fallback is the flag
// that continues the most recent conversation rather than a guessed id.
func TestSessionResumeFallsBackWithoutASessionID(t *testing.T) {
	ctx, out := testSessionContext(t)

	if code, err := runSession(context.Background(), ctx, []string{"provider", "zai"}); err != nil || code != 0 {
		t.Fatalf("runSession = (%d, %v), want (0, nil)", code, err)
	}

	printed := out.String()
	if !strings.Contains(printed, "clother zai --continue") {
		t.Errorf("output does not contain the continue fallback:\n%s", printed)
	}
	if strings.Contains(printed, "--resume") {
		t.Errorf("output offers a resume id it cannot know:\n%s", printed)
	}
}

// The session's own provider is reported from what the launcher left in the
// environment, which is the only record of it once Claude Code is running.
func TestSessionReportsTheRunningProvider(t *testing.T) {
	ctx, out := testSessionContext(t)
	t.Setenv("CLOTHER_PROFILE", "zai")

	if code, err := runSession(context.Background(), ctx, []string{"provider", "kimi"}); err != nil || code != 0 {
		t.Fatalf("runSession = (%d, %v), want (0, nil)", code, err)
	}

	printed := out.String()
	if !strings.Contains(printed, "still running on: zai") {
		t.Errorf("output does not report the running provider:\n%s", printed)
	}
}

func TestSessionConfigReportsAndDoesNotPrompt(t *testing.T) {
	ctx, out := testSessionContext(t)

	code, err := runSession(context.Background(), ctx, []string{"config", "zai"})
	if err != nil {
		t.Fatalf("runSession error = %v", err)
	}
	if code != 0 {
		t.Fatalf("runSession code = %d, want 0", code)
	}

	printed := out.String()
	if !strings.Contains(printed, "clother config zai") {
		t.Errorf("output does not name the command to run in a terminal:\n%s", printed)
	}
	if !strings.Contains(printed, "ZAI_API_KEY") {
		t.Errorf("output does not report the credential state:\n%s", printed)
	}
	configNotWritten(t, ctx)
}
