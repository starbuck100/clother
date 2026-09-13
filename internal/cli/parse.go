package cli

import "fmt"

type Options struct {
	Help     bool
	Version  bool
	Verbose  bool
	Debug    bool
	Quiet    bool
	Yes      bool
	NoInput  bool
	NoBanner bool
	// NoShim installs the provider launchers but leaves `claude` alone: the real
	// Claude Code keeps its name and no shim is written.
	NoShim bool
	BinDir string
	Format string
}

type Parsed struct {
	Options Options
	Command string
	Args    []string
}

// Parse splits clother's own command line into options, a command and its
// arguments.
//
// The option table in spec.go is the single source of truth for what an option
// is and what it sets; this function only walks the arguments. Everything the
// launcher path needs to tell apart — a clother option, a command, a token to
// hand to Claude Code — is answered from that same table, so the two can never
// disagree about a spelling.
func Parse(args []string) (Parsed, error) {
	parsed := Parsed{Options: Options{Format: "human"}}
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]

		// Everything after -- is positional. This is checked before the table,
		// because otherwise the terminator itself would look like an unknown
		// option and be refused.
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}

		spec, ok := Lookup(arg)
		if !ok {
			if len(arg) > 0 && arg[0] == '-' {
				return Parsed{}, fmt.Errorf("unknown option %s", arg)
			}
			positional = append(positional, arg)
			continue
		}

		// A value-taking option consumes the following argument whatever it
		// looks like: --bin-dir --json means the directory named "--json".
		value := ""
		if spec.TakesValue {
			if i+1 >= len(args) {
				return Parsed{}, fmt.Errorf("%s requires %s", spec.Name, spec.ValueHint)
			}
			i++
			value = args[i]
		}
		spec.Apply(&parsed.Options, value)
	}

	if len(positional) > 0 {
		parsed.Command = positional[0]
		parsed.Args = positional[1:]
	}
	return parsed, nil
}

// ParseLauncher is the launcher entry point, whose contract is the opposite of
// Parse's: it accepts --no-banner for clother itself and forwards everything
// else to Claude Code untouched, options included. A launcher invocation is
// selected by the name the binary was called under, not by an option, so
// nothing here may consume or reject one.
func ParseLauncher(args []string) (Options, []string) {
	options := Options{}
	var forwarded []string
	for _, arg := range args {
		if arg == "--no-banner" {
			options.NoBanner = true
			continue
		}
		forwarded = append(forwarded, arg)
	}
	return options, forwarded
}
