package commands

import (
	"context"
	"fmt"
	"sort"

	"github.com/jolehuit/clother/internal/cli"
)

// dispatch maps a subcommand name to its implementation.
//
// A table rather than a switch, so the set of names can be compared with the one
// the launch routing recognises. A name in one list and not in the other is
// either unreachable or undefined, and TestCommandsMatchTheRoutingRegister is
// what keeps them equal. The uniform signature is why a command that takes no
// arguments of its own is wrapped here rather than special-cased at the call
// site.
var dispatch = map[string]func(context.Context, Context, []string) (int, error){
	"bench":     runBench,
	"usage":     runUsage,
	"config":    runConfig,
	"info":      runInfo,
	"test":      runTest,
	"__session": runSession,
	"doctor":    runDoctor,
	"list":      withoutArgs(runList),
	"status":    withoutArgs(runStatus),
	"install":   withoutArgs(runInstall),
	"update":    withoutArgs(runUpdate),
	"uninstall": withoutArgs(runUninstall),
	"help":      showHelp,
}

// withoutArgs adapts a command that takes no arguments of its own.
func withoutArgs(run func(context.Context, Context) (int, error)) func(context.Context, Context, []string) (int, error) {
	return func(ctx context.Context, c Context, _ []string) (int, error) {
		return run(ctx, c)
	}
}

func showHelp(_ context.Context, c Context, _ []string) (int, error) {
	cli.ShowFull(c.Output.Stdout, c.Catalog)
	return 0, nil
}

// Dispatch runs a subcommand by name. An empty name is not an error: it is what
// the parser produces for an invocation with no command, and it has always
// printed the brief.
func Dispatch(ctx context.Context, c Context, command string, args []string) (int, error) {
	if command == "" {
		cli.ShowBrief(c.Output.Stdout)
		return 0, nil
	}
	run, ok := dispatch[command]
	if !ok {
		return 1, fmt.Errorf("unknown command %q", command)
	}
	return run(ctx, c, args)
}

// CommandNames lists the subcommands this package implements, sorted.
func CommandNames() []string {
	names := make([]string, 0, len(dispatch))
	for name := range dispatch {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
