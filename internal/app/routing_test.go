package app

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/jolehuit/clother/internal/cli"
	"github.com/jolehuit/clother/internal/profiles"
)

// stubSet stands in for the catalog and the user's configuration. These rules
// are about what an invocation means, not about which providers happen to exist,
// so a set of names is all the environment the table needs.
type stubSet struct {
	providers []string
	freeModel []string
	commands  []string
}

func (s stubSet) IsProvider(name string) bool {
	return slices.Contains(s.providers, name)
}

func (s stubSet) IsModelTagProvider(name string) bool {
	return slices.Contains(s.freeModel, name)
}

func (s stubSet) Candidates() []string {
	return append(slices.Clone(s.commands), s.providers...)
}

// testSet mirrors the shape of a real installation: the subscription, a couple
// of catalog providers with fixed model lists, an OpenRouter profile whose model
// is a free tag, and one configured alias.
func testSet() stubSet {
	return stubSet{
		providers: []string{"native", "zai", "openrouter", "or-kimi"},
		freeModel: []string{"openrouter", "or-kimi"},
		commands:  append(cli.CommandNames(), profiles.AliasNames()...),
	}
}

// checkDecision compares everything except Args, which is compared with
// slices.Equal so that a nil slice and an empty one are interchangeable. Which
// of the two falls out of a slice expression is not a contract.
func checkDecision(t *testing.T, args []string, got, want Decision) {
	t.Helper()

	if got.Mode != want.Mode {
		t.Errorf("Decide(%q) Mode = %v, want %v", args, got.Mode, want.Mode)
	}
	if got.Provider != want.Provider {
		t.Errorf("Decide(%q) Provider = %q, want %q", args, got.Provider, want.Provider)
	}
	if got.Model != want.Model {
		t.Errorf("Decide(%q) Model = %q, want %q", args, got.Model, want.Model)
	}
	if !reflect.DeepEqual(got.Options, want.Options) {
		t.Errorf("Decide(%q) Options = %#v, want %#v", args, got.Options, want.Options)
	}
	if !slices.Equal(got.Args, want.Args) {
		t.Errorf("Decide(%q) Args = %q, want %q", args, got.Args, want.Args)
	}
}

func TestDecide(t *testing.T) {
	t.Parallel()

	set := testSet()

	tests := []struct {
		name string
		args []string
		want Decision
	}{
		{
			name: "bare clother launches",
			args: nil,
			want: Decision{Mode: ModeLaunch},
		},
		{
			name: "a claude option alone launches with it",
			args: []string{"--yolo"},
			want: Decision{Mode: ModeLaunch, Args: []string{"--yolo"}},
		},
		{
			name: "resume is not an unknown option any more",
			args: []string{"--resume", "abc"},
			want: Decision{Mode: ModeLaunch, Args: []string{"--resume", "abc"}},
		},
		{
			name: "a provider name selects that provider",
			args: []string{"zai"},
			want: Decision{Mode: ModeLaunch, Provider: "zai"},
		},
		{
			name: "a provider name and claude arguments",
			args: []string{"zai", "--resume", "abc"},
			want: Decision{Mode: ModeLaunch, Provider: "zai", Args: []string{"--resume", "abc"}},
		},
		{
			// The alias has to be resolved before the provider is looked up, or
			// the subscription's own name becomes a prompt.
			name: "the claude alias selects the subscription",
			args: []string{"claude"},
			want: Decision{Mode: ModeLaunch, Provider: "native"},
		},
		{
			name: "a clother option comes along to the launch",
			args: []string{"--bin-dir", "/somewhere/bin", "zai"},
			want: Decision{Mode: ModeLaunch, Provider: "zai", Options: cli.Options{BinDir: "/somewhere/bin"}},
		},
		{
			name: "a prompt with spaces is not a provider",
			args: []string{"fix the bug"},
			want: Decision{Mode: ModeLaunch, Args: []string{"fix the bug"}},
		},
		{
			name: "the terminator forwards an option-looking token",
			args: []string{"--", "--json"},
			want: Decision{Mode: ModeLaunch, Args: []string{"--json"}},
		},
		{
			name: "the terminator beats a near miss",
			args: []string{"--", "hlep"},
			want: Decision{Mode: ModeLaunch, Args: []string{"hlep"}},
		},
		{
			// The escape hatch for an option that was refused: after `--` it is
			// Claude Code's, not clother's.
			name: "the terminator forwards a refused option",
			args: []string{"--", "--verbose"},
			want: Decision{Mode: ModeLaunch, Args: []string{"--verbose"}},
		},
		{
			name: "a free model tag after a provider is read as the model",
			args: []string{"openrouter", "moonshotai/kimi-k2.6"},
			want: Decision{Mode: ModeLaunch, Provider: "openrouter", Model: "moonshotai/kimi-k2.6"},
		},
		{
			name: "a model tag and claude arguments together",
			args: []string{"openrouter", "moonshotai/kimi-k2.6", "--yolo"},
			want: Decision{Mode: ModeLaunch, Provider: "openrouter", Model: "moonshotai/kimi-k2.6", Args: []string{"--yolo"}},
		},
		{
			// The reason the model tag is gated on the provider: for one with a
			// fixed model list there is no tag to name, so this is a prompt.
			name: "a path-like token stays a prompt for a fixed-model provider",
			args: []string{"zai", "src/main.go"},
			want: Decision{Mode: ModeLaunch, Provider: "zai", Args: []string{"src/main.go"}},
		},
		{
			name: "an option after a free-tag provider is not a model",
			args: []string{"openrouter", "--yolo"},
			want: Decision{Mode: ModeLaunch, Provider: "openrouter", Args: []string{"--yolo"}},
		},
		{
			name: "a token that is not tag-shaped is not a model",
			args: []string{"openrouter", "/not/a/tag"},
			want: Decision{Mode: ModeLaunch, Provider: "openrouter", Args: []string{"/not/a/tag"}},
		},
		{
			name: "a configured alias provider reads a tag too",
			args: []string{"or-kimi", "moonshotai/kimi-k2.6"},
			want: Decision{Mode: ModeLaunch, Provider: "or-kimi", Model: "moonshotai/kimi-k2.6"},
		},
		{
			name: "a subcommand is still a subcommand",
			args: []string{"list"},
			want: Decision{Mode: ModeCommand},
		},
		{
			name: "a subcommand with an argument",
			args: []string{"config", "zai"},
			want: Decision{Mode: ModeCommand},
		},
		{
			name: "the hidden helper is a subcommand",
			args: []string{"__session", "provider"},
			want: Decision{Mode: ModeCommand},
		},
		{
			// Refusal is for launches only: --json is perfectly meaningful here.
			name: "clother options are fine before a subcommand",
			args: []string{"--json", "list"},
			want: Decision{Mode: ModeCommand, Options: cli.Options{Format: "json"}},
		},
		{
			name: "help is a subcommand",
			args: []string{"help"},
			want: Decision{Mode: ModeCommand},
		},
		{
			name: "help option prints clother's help",
			args: []string{"--help"},
			want: Decision{Mode: ModeHelp, Options: cli.Options{Help: true}},
		},
		{
			name: "short help option",
			args: []string{"-h"},
			want: Decision{Mode: ModeHelp, Options: cli.Options{Help: true}},
		},
		{
			name: "version option",
			args: []string{"--version"},
			want: Decision{Mode: ModeVersion, Options: cli.Options{Version: true}},
		},
		{
			name: "short version option",
			args: []string{"-V"},
			want: Decision{Mode: ModeVersion, Options: cli.Options{Version: true}},
		},
		{
			// A clother-only option with nothing to launch: today's brief, with
			// the option applied.
			name: "a lone clother option prints the brief",
			args: []string{"--no-banner"},
			want: Decision{Mode: ModeBrief, Options: cli.Options{NoBanner: true}},
		},
		{
			name: "a lone output option prints the brief",
			args: []string{"--json"},
			want: Decision{Mode: ModeBrief, Options: cli.Options{Format: "json"}},
		},
		{
			// `clother -v` keeps meaning what it has always meant: clother's own
			// verbose output, with nothing to launch so the brief is printed.
			name: "a lone verbose option prints the brief",
			args: []string{"-v"},
			want: Decision{Mode: ModeBrief, Options: cli.Options{Verbose: true}},
		},
		{
			// Help wins over a provider name, which is what it has always done:
			// `clother zai --help` never launched anything.
			name: "help wins over a provider name",
			args: []string{"zai", "--help"},
			want: Decision{Mode: ModeHelp, Options: cli.Options{Help: true}},
		},
		{
			name: "version wins over a provider name",
			args: []string{"zai", "--version"},
			want: Decision{Mode: ModeVersion, Options: cli.Options{Version: true}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Decide(tt.args, set)
			if err != nil {
				t.Fatalf("Decide(%q) error = %v, want nil", tt.args, err)
			}
			checkDecision(t, tt.args, got, tt.want)
		})
	}
}

func TestDecideErrors(t *testing.T) {
	t.Parallel()

	set := testSet()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "a mistyped command is reported with a suggestion",
			args: []string{"hlep"},
			want: `did you mean "help"`,
		},
		{
			name: "a mistyped command one letter short",
			args: []string{"lst"},
			want: `did you mean "list"`,
		},
		{
			name: "a transposed command",
			args: []string{"statsu"},
			want: `did you mean "status"`,
		},
		{
			name: "a mistyped alias suggests the alias",
			args: []string{"claud"},
			want: `did you mean "claude"`,
		},
		{
			// The refusal has to be reachable, or the flag would be dropped in
			// silence and the user would never learn why it did nothing.
			name: "a command-only option is refused for a launch",
			args: []string{"--json", "zai"},
			want: "--json applies to clother commands only",
		},
		{
			name: "another command-only option is refused for a launch",
			args: []string{"--plain", "openrouter"},
			want: "--plain applies to clother commands only",
		},
		{
			// A name Claude Code also has, but clother's output flag: a launch
			// has no use for it, so it says so rather than ignoring it.
			name: "verbose is refused for a launch",
			args: []string{"zai", "--verbose"},
			want: "--verbose applies to clother commands only",
		},
		{
			name: "quiet is refused for a launch",
			args: []string{"zai", "-q"},
			want: "--quiet applies to clother commands only",
		},
		{
			name: "a missing option value is reported",
			args: []string{"--bin-dir"},
			want: "--bin-dir requires a path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Decide(tt.args, set)
			if err == nil {
				t.Fatalf("Decide(%q) error = nil, want %q", tt.args, tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Decide(%q) error = %q, want it to contain %q", tt.args, err, tt.want)
			}
		})
	}
}

// The escape hatch the refusal and the near-miss message both name has to work,
// which is what makes refusing safe: no input is unreachable.
func TestNearMissEscapeHatchWorks(t *testing.T) {
	t.Parallel()

	set := testSet()

	if _, err := Decide([]string{"hlep"}, set); err == nil {
		t.Fatal("a near miss was not reported")
	}

	got, err := Decide([]string{"--", "hlep"}, set)
	if err != nil {
		t.Fatalf("the escape hatch failed: %v", err)
	}
	if !slices.Equal(got.Args, []string{"hlep"}) {
		t.Errorf("Args = %q, want the token forwarded", got.Args)
	}
}
