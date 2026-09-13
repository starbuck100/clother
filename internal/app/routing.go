package app

import (
	"fmt"

	"github.com/jolehuit/clother/internal/cli"
	"github.com/jolehuit/clother/internal/profiles"
)

// Mode is what an invocation turned out to mean.
type Mode int

const (
	// ModeLaunch runs Claude Code, under the remembered provider or the one
	// named on the command line.
	ModeLaunch Mode = iota
	// ModeCommand runs one of clother's own subcommands.
	ModeCommand
	// ModeBrief, ModeHelp and ModeVersion print clother's own text.
	ModeBrief
	ModeHelp
	ModeVersion
)

// Decision is a routing outcome. It carries no side effects and does no I/O:
// deciding and doing are separate so the rules can be a table in a test.
type Decision struct {
	Mode Mode
	// Provider is the provider named on the command line, already mapped through
	// profiles.ResolveAlias. Empty means the remembered one is to be used.
	Provider string
	// Model is the model tag that followed the provider. Empty means the
	// provider's own default.
	Model string
	// Args is what reaches Claude Code: clother's own options removed and the
	// `--` terminator stripped.
	Args []string
	// Options are the clother options that were consumed.
	Options cli.Options
}

// ProviderSet is what the router needs to know about the user's providers. It is
// an interface rather than the catalog itself so the rules can be tested as a
// table, with no catalog and no configuration on disk.
type ProviderSet interface {
	// IsProvider reports whether name names a provider the user has configured.
	IsProvider(name string) bool
	// IsModelTagProvider reports whether a provider's model is a free tag rather
	// than an entry in a fixed list. That is what decides whether a bare
	// vendor/model token after the provider name was meant as a model or as the
	// beginning of a prompt.
	IsModelTagProvider(name string) bool
	// Candidates lists the names a typo could have been aimed at: clother's own
	// commands plus every configured provider.
	Candidates() []string
}

// Decide works out what an invocation means.
//
// The rule that makes a bare `clother` launch is that a token is a subcommand
// only when it is one of clother's own. Anything else is Claude Code's: a
// provider to launch under, or the beginning of its arguments. Where the two
// readings collide — `clother fix the bug` against `clother zai` — the provider
// reading wins, because `clother zai --resume x` has to work, and `--` is the
// documented way to say "this is Claude Code's".
func Decide(args []string, set ProviderSet) (Decision, error) {
	split, err := cli.SplitClotherOptions(args)
	if err != nil {
		return Decision{}, err
	}

	// Help and version win wherever they appear, which is what they have always
	// done: `clother zai --help` printed clother's own help rather than
	// launching. Asking for text is unambiguous in a way that a launch is not.
	if split.Options.Help {
		return Decision{Mode: ModeHelp, Options: split.Options}, nil
	}
	if split.Options.Version {
		return Decision{Mode: ModeVersion, Options: split.Options}, nil
	}

	// A bare `clother` launches. Anything else with nothing left to launch with
	// is clother's own text.
	if len(args) == 0 {
		return Decision{Mode: ModeLaunch}, nil
	}
	if len(split.Rest) == 0 {
		return Decision{Mode: ModeBrief, Options: split.Options}, nil
	}

	// The original arguments go to the parser untouched, so every command path
	// behaves exactly as it did before any of this routing existed.
	if cli.IsCommand(split.Rest[0]) {
		return Decision{Mode: ModeCommand, Options: split.Options}, nil
	}

	if split.Rest[0] == "--" {
		return Decision{Mode: ModeLaunch, Args: split.Rest[1:], Options: split.Options}, nil
	}

	// Past this point the invocation is a launch, and a launch cannot honour an
	// option that only means something to one of clother's own commands.
	if refused, ok := split.Refused(); ok {
		return Decision{}, fmt.Errorf("option %s applies to clother commands only; run clother help", refused)
	}

	// The alias is resolved before the provider is looked up, not after: `claude`
	// is not a provider id, so asking the set about the raw token would send the
	// subscription's own name to Claude Code as a prompt.
	rest := split.Rest
	provider := profiles.ResolveAlias(rest[0])
	if !set.IsProvider(provider) {
		if hint, ok := cli.NearMiss(rest[0], set.Candidates()); ok {
			return Decision{}, fmt.Errorf(
				"unknown command %q; did you mean %q? To send that text to Claude Code, run: clother -- %s",
				rest[0], hint, rest[0])
		}
		return Decision{Mode: ModeLaunch, Args: rest, Options: split.Options}, nil
	}

	decision := Decision{Mode: ModeLaunch, Provider: provider, Options: split.Options}
	rest = rest[1:]

	// A model tag is read directly after the provider, and only for a provider
	// whose model is a free tag. For one with a fixed model list the token is
	// left alone, so `clother zai src/main.go` is a prompt and not a model named
	// after a file.
	if len(rest) > 0 && set.IsModelTagProvider(provider) && profiles.IsModelTag(rest[0]) {
		decision.Model = rest[0]
		rest = rest[1:]
	}

	decision.Args = rest
	return decision, nil
}
