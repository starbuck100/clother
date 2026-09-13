package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jolehuit/clother/internal/cli"
	"github.com/jolehuit/clother/internal/commands"
	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/platform"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
	"github.com/jolehuit/clother/internal/runtime"
	"github.com/jolehuit/clother/internal/ui"
	"github.com/jolehuit/clother/internal/update"
	"github.com/jolehuit/clother/internal/version"
)

type App struct {
	Options cli.Options
	Paths   config.Paths
	Config  *config.File
	Secrets config.Secrets
	Catalog providers.Catalog
	Output  *ui.Output
	Prompt  *ui.Prompter
}

// New loads everything a command needs. It takes the parsed options rather than
// the whole parse result so that the routing decision, which has to be made
// before the arguments are parsed, can construct one too.
func New(options cli.Options) (*App, error) {
	paths, err := config.Detect(options.BinDir)
	if err != nil {
		return nil, err
	}
	catalog, err := providers.Load()
	if err != nil {
		return nil, err
	}
	secrets, err := config.LoadSecrets(paths.SecretsFile)
	if err != nil {
		return nil, err
	}
	cfg, err := config.LoadConfig(paths.ConfigFile)
	if err != nil {
		return nil, err
	}
	cfg.ApplyLegacySecrets(secrets, catalog)
	_ = config.MigrateLegacyLaunchers(paths.BinDir, catalog, cfg)
	cfg.Normalize(catalog)

	output := ui.New(ui.Format(options.Format), options.Quiet)
	return &App{
		Options: options,
		Paths:   paths,
		Config:  cfg,
		Secrets: secrets,
		Catalog: catalog,
		Output:  output,
		Prompt:  ui.NewPrompter(os.Stdin, os.Stdout),
	}, nil
}

// Context is what a subcommand needs, so a caller does not have to know which of
// these fields is for it.
func (a *App) Context() commands.Context {
	return commands.Context{
		Paths:   a.Paths,
		Config:  a.Config,
		Secrets: a.Secrets,
		Catalog: a.Catalog,
		Output:  a.Output,
		Prompt:  a.Prompt,
		Options: a.Options,
	}
}

func Run(ctx context.Context, args []string, argv0 string) (int, error) {
	if platform.IsClaudeName(argv0) {
		paths, err := config.Detect("")
		if err != nil {
			return 1, err
		}
		return runtime.RunClaudeShim(ctx, paths, args)
	}

	if profile, isLauncher := profiles.Invocation(argv0); isLauncher {
		return runLauncher(ctx, profile, args)
	}

	// Neither the shim nor a launcher, so this is clother's own command line.
	// What it means is worked out before the arguments are parsed, because the
	// parser rejects the arguments that belong to Claude Code: `clother --yolo`
	// and `clother --resume x` are launches, not unknown options.
	split, err := cli.SplitClotherOptions(args)
	if err != nil {
		return 1, err
	}
	app, err := New(split.Options)
	if err != nil {
		return 1, err
	}

	decision, err := Decide(args, app.providerSet())
	if err != nil {
		fmt.Fprintln(app.Output.Stderr, err)
		return 1, nil
	}

	switch decision.Mode {
	case ModeHelp:
		cli.ShowFull(app.Output.Stdout, app.Catalog)
		return 0, nil
	case ModeVersion:
		fmt.Fprintf(app.Output.Stdout, "Clother v%s\n", version.Value)
		return 0, nil
	case ModeBrief:
		cli.ShowBrief(app.Output.Stdout)
		return 0, nil
	case ModeCommand:
		return runCommand(ctx, app, args)
	default:
		return app.launch(ctx, decision)
	}
}

// runLauncher is the path taken when clother is invoked under one of its launcher
// names — clother-zai and the rest — where the provider comes from the name the
// binary was called under.
func runLauncher(ctx context.Context, profile string, args []string) (int, error) {
	options, forwarded := cli.ParseLauncher(args)
	paths, err := config.Detect("")
	if err != nil {
		return 1, err
	}
	catalog, err := providers.Load()
	if err != nil {
		return 1, err
	}
	secrets, err := config.LoadSecrets(paths.SecretsFile)
	if err != nil {
		return 1, err
	}
	cfg, err := config.LoadConfig(paths.ConfigFile)
	if err != nil {
		return 1, err
	}
	cfg.ApplyLegacySecrets(secrets, catalog)
	cfg.Normalize(catalog)

	// clother-or <alias> and clother-custom <name> name a provider indirectly.
	if profiles.IsGateway(profile) {
		resolved, remaining, err := profiles.Gateway(profile, forwarded, cfg)
		if err != nil {
			// A missing name is a usage problem, not a failure: this has always
			// printed the usage and exited without an error message.
			var usage *profiles.GatewayUsageError
			if errors.As(err, &usage) {
				fmt.Fprintln(os.Stderr, usage.Usage)
				return 1, nil
			}
			return 1, err
		}
		profile, forwarded = resolved, remaining
	}

	target, err := profiles.Resolve(profile, catalog, cfg)
	if err != nil {
		return 1, err
	}
	return commands.RunLauncher(ctx, paths, secrets, target, forwarded, options.NoBanner)
}

// runCommand runs one of clother's own subcommands. The arguments go to the
// parser unchanged, so every command path behaves exactly as it did before the
// routing existed.
func runCommand(ctx context.Context, app *App, args []string) (int, error) {
	parsed, err := cli.Parse(args)
	if err != nil {
		return 1, err
	}
	if parsed.Options.Format == "human" && !parsed.Options.Quiet && parsed.Command != "install" && parsed.Command != "uninstall" {
		if message, err := update.MaybeMessage(app.Paths, version.Value, time.Now()); err == nil && message != "" {
			fmt.Fprintln(app.Output.Stderr, message)
		}
	}
	return commands.Dispatch(ctx, app.Context(), parsed.Command, parsed.Args)
}

// launch runs Claude Code under the selected provider.
func (a *App) launch(ctx context.Context, decision Decision) (int, error) {
	selection, err := profiles.Active(a.Catalog, a.Config)
	if err != nil {
		return 1, err
	}
	if decision.Provider != "" {
		target, err := profiles.Resolve(decision.Provider, a.Catalog, a.Config)
		if err != nil {
			return 1, err
		}
		selection = profiles.Selection{Target: target, Model: decision.Model}
	}

	if selection.Note != "" {
		fmt.Fprintln(a.Output.Stderr, selection.Note)
	}
	if err := a.remember(decision, selection); err != nil {
		// Not being able to remember the choice is not a reason to refuse the
		// launch that was actually asked for.
		a.Output.Warn("%v", err)
	}

	// The remembered model travels as --model, so it reaches Claude Code through
	// exactly the path an explicit --model takes and is never written into the
	// configuration as a provider override.
	args := decision.Args
	if selection.Model != "" && runtime.ModelOverride(args) == "" {
		args = append([]string{"--model", selection.Model}, args...)
	}
	return commands.RunLauncher(ctx, a.Paths, a.Secrets, selection.Target, args, decision.Options.NoBanner)
}

// remember stores what the command line selected, so that the next bare
// `clother` launches the same thing.
//
// The first bare launch stores the default instead of leaving the remembered
// state empty, and says which provider it is starting. The alternative is a
// silent default that the user cannot tell apart from a remembered choice, and
// the launch happens either way.
func (a *App) remember(decision Decision, selection profiles.Selection) error {
	switch {
	case decision.Provider != "":
		return config.SetActive(a.Paths.ConfigFile, decision.Provider, decision.Model)
	case a.Config.Active == nil:
		if err := config.SetActive(a.Paths.ConfigFile, selection.Target.Profile, ""); err != nil {
			return err
		}
		a.Output.Line("Clother: launching %s. Run `clother help` for clother's own commands.", selection.Target.DisplayName)
	}
	return nil
}

// providerSet answers the routing questions from the loaded catalog and
// configuration.
func (a *App) providerSet() ProviderSet {
	return providerSet{catalog: a.Catalog, config: a.Config}
}

type providerSet struct {
	catalog providers.Catalog
	config  *config.File
}

func (p providerSet) IsProvider(name string) bool {
	if _, ok := p.catalog.Get(name); ok {
		return true
	}
	if p.config == nil {
		return false
	}
	if alias, ok := strings.CutPrefix(name, "or-"); ok {
		return p.config.OpenRouterAliases[alias] != ""
	}
	_, ok := p.config.CustomProviders[name]
	return ok
}

// IsModelTagProvider reports whether a provider's model is a free tag. Those are
// the families whose models are not a fixed list: OpenRouter and anything the
// user configured themselves.
func (p providerSet) IsModelTagProvider(name string) bool {
	target, err := profiles.Resolve(name, p.catalog, p.config)
	if err != nil {
		return false
	}
	return target.Family == providers.FamilyOpenRouter || target.Family == providers.FamilyCustomUnknown
}

func (p providerSet) Gateway(kind string, args []string) (string, []string, error) {
	return profiles.Gateway(kind, args, p.config)
}

func (p providerSet) Candidates() []string {
	names := cli.CommandNames()
	names = append(names, profiles.AliasNames()...)
	for _, target := range profiles.All(p.catalog, p.config) {
		names = append(names, target.Profile)
	}
	return names
}
