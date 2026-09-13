package cli

import "fmt"

// OptionSpec describes one option clother accepts: its canonical name, the short
// spellings that mean the same thing, whether it consumes the following
// argument, and what setting it has on Options.
//
// The table exists so that the questions the launch routing has to answer — "is
// this token one of clother's own options?" and "is this token a command?" — are
// answered from one source instead of from the switch inside Parse.
//
// Several of these names exist in Claude Code as well: -h/--help, -v/--verbose
// and -d/--debug all do. They are still clother's here, because a token
// immediately after `clother` belongs to clother, and `clother --help` printing
// Claude Code's help would be a surprise. Passing one through is what the `--`
// terminator is for.
type OptionSpec struct {
	// Name is the canonical long spelling, and the one used in error messages.
	Name string
	// Aliases are the other spellings that select this option.
	Aliases []string
	// TakesValue means the option consumes the following argument.
	TakesValue bool
	// ValueHint names the consumed value for the missing-value error, as in
	// "requires a path". Only meaningful with TakesValue.
	ValueHint string
	// CommandOnly marks an option that only means something to one of clother's
	// own commands. A launch cannot honour --json or --verbose, and accepting
	// them and then ignoring them would be worse than refusing them by name.
	CommandOnly bool
	// Apply sets the option on Options. The value is empty unless TakesValue.
	Apply func(*Options, string)
}

// specList is the single source of truth for clother's own options. Parse walks
// it, and so does SplitClotherOptions.
func specList() []OptionSpec {
	return []OptionSpec{
		{
			Name:    "--help",
			Aliases: []string{"-h"},
			Apply:   func(o *Options, _ string) { o.Help = true },
		},
		{
			// -V is version, -v is verbose. The capitals are the whole
			// distinction, and Claude Code spells it the other way round:
			// `claude -v` is its version while `claude --verbose` is its
			// verbose. Keeping -V here means `clother --version` answers "which
			// clother is this" and `clother -v` stays what it has always been.
			Name:    "--version",
			Aliases: []string{"-V"},
			Apply:   func(o *Options, _ string) { o.Version = true },
		},
		{
			Name:        "--verbose",
			Aliases:     []string{"-v"},
			CommandOnly: true,
			Apply:       func(o *Options, _ string) { o.Verbose = true },
		},
		{
			// Debug implies verbose: the verbose stream is what a debug report
			// is meant to be read with.
			Name:        "--debug",
			Aliases:     []string{"-d"},
			CommandOnly: true,
			Apply: func(o *Options, _ string) {
				o.Debug = true
				o.Verbose = true
			},
		},
		{
			Name:        "--quiet",
			Aliases:     []string{"-q"},
			CommandOnly: true,
			Apply:       func(o *Options, _ string) { o.Quiet = true },
		},
		{
			Name:        "--yes",
			Aliases:     []string{"-y"},
			CommandOnly: true,
			Apply:       func(o *Options, _ string) { o.Yes = true },
		},
		{
			Name:        "--no-input",
			CommandOnly: true,
			Apply:       func(o *Options, _ string) { o.NoInput = true },
		},
		{
			// The launch honours this one: it is what suppresses clother's own
			// banner before Claude Code starts.
			Name:  "--no-banner",
			Apply: func(o *Options, _ string) { o.NoBanner = true },
		},
		{
			Name:        "--no-shim",
			CommandOnly: true,
			Apply:       func(o *Options, _ string) { o.NoShim = true },
		},
		{
			Name:        "--no-commands",
			CommandOnly: true,
			Apply:       func(o *Options, _ string) { o.NoCommands = true },
		},
		{
			Name:        "--json",
			CommandOnly: true,
			Apply:       func(o *Options, _ string) { o.Format = "json" },
		},
		{
			Name:        "--plain",
			CommandOnly: true,
			Apply:       func(o *Options, _ string) { o.Format = "plain" },
		},
		{
			// The launch honours this one too: it decides which directory the
			// providers are resolved from.
			Name:       "--bin-dir",
			TakesValue: true,
			ValueHint:  "a path",
			Apply:      func(o *Options, value string) { o.BinDir = value },
		},
	}
}

// Specs returns the option table. It is rebuilt per call so a caller cannot
// reorder or mutate the table the parser walks.
func Specs() []OptionSpec {
	return specList()
}

// Lookup finds the option a token selects, by canonical name or alias.
func Lookup(arg string) (OptionSpec, bool) {
	if arg == "" {
		return OptionSpec{}, false
	}
	for _, spec := range specList() {
		if arg == spec.Name {
			return spec, true
		}
		for _, alias := range spec.Aliases {
			if arg == alias {
				return spec, true
			}
		}
	}
	return OptionSpec{}, false
}

// Split is what an invocation looks like once clother's own options are taken
// out of it.
type Split struct {
	// Options holds the options that were consumed.
	Options Options
	// Consumed records which options were consumed, in the order they appeared,
	// so the caller can name the one it has to refuse.
	Consumed []OptionSpec
	// Rest is everything left, unchanged and in order: unknown options,
	// positionals, and the `--` terminator with whatever follows it.
	Rest []string
}

// Refused returns the first consumed option that only means something to one of
// clother's own commands.
func (s Split) Refused() (string, bool) {
	for _, spec := range s.Consumed {
		if spec.CommandOnly {
			return spec.Name, true
		}
	}
	return "", false
}

// SplitClotherOptions consumes every option clother knows and leaves everything
// else alone.
//
// All of them, including the names Claude Code shares: an option written next to
// `clother` was meant for clother. What is left is the material for the routing
// decision — unknown options, positionals, and a `--` terminator retained with
// its tail so the caller can still see a leading `--` and act on it.
func SplitClotherOptions(args []string) (Split, error) {
	var split Split

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if arg == "--" {
			split.Rest = append(split.Rest, args[i:]...)
			break
		}

		spec, ok := Lookup(arg)
		if !ok {
			split.Rest = append(split.Rest, arg)
			continue
		}

		value := ""
		if spec.TakesValue {
			if i+1 >= len(args) {
				return Split{}, fmt.Errorf("%s requires %s", spec.Name, spec.ValueHint)
			}
			i++
			value = args[i]
		}
		spec.Apply(&split.Options, value)
		split.Consumed = append(split.Consumed, spec)
	}

	return split, nil
}
