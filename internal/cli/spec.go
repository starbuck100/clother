package cli

import "fmt"

// OptionSpec describes one option clother accepts: its canonical name, the short
// spellings that mean the same thing, whether it consumes the next argument, and
// what setting it has on Options.
//
// The table exists so that the two questions the launch routing has to answer —
// "is this token one of clother's own options?" and "is this token a command?"
// — are answered from one source instead of from the switch inside Parse.
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
	// Shared marks a name that Claude Code accepts too: -h/--help, -v/--verbose
	// and -d/--debug exist in both. A shared name must never be used to decide
	// which mode an invocation is in, because "this is a known clother option"
	// would then turn `clother --verbose` into a different command than it is
	// today.
	Shared bool
	// Launch marks an option that still means something when clother launches
	// Claude Code instead of running a command of its own. Options that are
	// false here only make sense for clother's own commands, so the launch path
	// refuses them rather than dropping a flag the user typed.
	Launch bool
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
			Shared:  true,
			Launch:  true,
			Apply:   func(o *Options, _ string) { o.Help = true },
		},
		{
			// -V is version, -v is verbose. The capitals are the whole
			// distinction, and Claude Code spells it the other way round:
			// `claude -v` is its verbose, `claude --version` is its version. So
			// --version is clother's answer to "which clother is this", while -v
			// stays verbose for both.
			Name:    "--version",
			Aliases: []string{"-V"},
			Apply:   func(o *Options, _ string) { o.Version = true },
		},
		{
			Name:    "--verbose",
			Aliases: []string{"-v"},
			Shared:  true,
			Launch:  true,
			Apply:   func(o *Options, _ string) { o.Verbose = true },
		},
		{
			// Debug implies verbose: the verbose stream is what a debug report
			// is meant to be read with.
			Name:    "--debug",
			Aliases: []string{"-d"},
			Shared:  true,
			Launch:  true,
			Apply:   func(o *Options, _ string) {
				o.Debug = true
				o.Verbose = true
			},
		},
		{
			Name:    "--quiet",
			Aliases: []string{"-q"},
			Launch:  true,
			Apply:   func(o *Options, _ string) { o.Quiet = true },
		},
		{
			Name:    "--yes",
			Aliases: []string{"-y"},
			Apply:   func(o *Options, _ string) { o.Yes = true },
		},
		{
			Name:  "--no-input",
			Apply: func(o *Options, _ string) { o.NoInput = true },
		},
		{
			Name:    "--no-banner",
			Launch:  true,
			Apply:   func(o *Options, _ string) { o.NoBanner = true },
		},
		{
			Name:  "--no-shim",
			Apply: func(o *Options, _ string) { o.NoShim = true },
		},
		{
			Name:  "--json",
			Apply: func(o *Options, _ string) { o.Format = "json" },
		},
		{
			Name:  "--plain",
			Apply: func(o *Options, _ string) { o.Format = "plain" },
		},
		{
			Name:       "--bin-dir",
			TakesValue: true,
			ValueHint:  "a path",
			Launch:     true,
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
	// Options holds the clother-only options that were consumed.
	Options Options
	// Consumed records which options were consumed, in the order they appeared,
	// so the caller can name the one it has to refuse.
	Consumed []OptionSpec
	// Rest is everything left, unchanged and in order: shared options, unknown
	// options, positionals, and the `--` terminator with whatever follows it.
	Rest []string
}

// Refused returns the first consumed option that has no meaning outside
// clother's own commands. A launch cannot honour --json, and dropping it
// silently would be worse than saying so.
func (s Split) Refused() (string, bool) {
	for _, spec := range s.Consumed {
		if !spec.Launch {
			return spec.Name, true
		}
	}
	return "", false
}

// SplitClotherOptions consumes the options only clother understands and leaves
// everything else alone.
//
// Only options exclusive to clother are consumed. The shared spellings
// (-h/--help, -v/--verbose, -d/--debug) are deliberately left in Rest even
// though clother would also accept them: they are a legitimate thing to pass
// through to Claude Code, and consuming them would change what
// `clother --verbose` does today. A `--` and everything after it is passed
// through untouched, so the caller can still see a leading `--` and act on it.
func SplitClotherOptions(args []string) (Split, error) {
	var split Split

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if arg == "--" {
			split.Rest = append(split.Rest, args[i:]...)
			break
		}

		spec, ok := Lookup(arg)
		if !ok || spec.Shared {
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
