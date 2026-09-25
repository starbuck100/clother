package commands

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
)

// runSession is the helper the generated slash commands call.
//
// It is a hidden surface on purpose. The files that reach it are written into
// the user's Claude configuration at install time, so keeping this separate from
// the commands a person types means those files do not depend on the
// user-facing surface staying still.
//
// Invalid input exits 2, so a caller can tell "you asked for something that
// cannot work" from "the switch failed".
func runSession(ctx context.Context, c Context, args []string) (int, error) {
	action := ""
	if len(args) > 0 {
		action = args[0]
	}

	switch action {
	case "next", "pin", "free", "auto":
		return sessionControl(ctx, c, action)
	case "statusline":
		return sessionStatusline(c)
	case "provider":
		return sessionProvider(c, args[1:])
	case "config":
		return sessionConfig(c, args[1:])
	case "usage":
		return runUsage(ctx, c, args[1:])
	case "status":
		return runStatus(ctx, c)
	default:
		return 2, fmt.Errorf("usage: clother __session <provider|config|status|usage> [args...]")
	}
}

func sessionProvider(c Context, args []string) (int, error) {
	if len(args) == 0 {
		active := activeSelection(c)
		return 0, reportProvider(c, active.Profile, active.Model)
	}

	name := strings.TrimSpace(args[0])
	if !profiles.IsProviderName(name) {
		return 2, fmt.Errorf("%q is not a provider name (lowercase letters, digits, \"-\" and \"_\" only)", name)
	}
	profile := profiles.ResolveAlias(name)

	model := ""
	if len(args) > 1 {
		model = strings.TrimSpace(args[1])
		if !profiles.IsModelTag(model) {
			return 2, fmt.Errorf("%q is not a model tag (expected <vendor>/<model>)", model)
		}
	}

	// Resolution is what validates the name: a well-formed name that is not a
	// configured provider is reported rather than stored.
	target, err := profiles.Resolve(profile, c.Catalog, c.Config)
	if err != nil {
		return 2, err
	}
	if err := config.SetActive(c.Paths.ConfigFile, target.Profile, model); err != nil {
		return 1, err
	}

	fmt.Fprintf(c.Output.Stdout, "remembered: %s", target.Profile)
	if model != "" {
		fmt.Fprintf(c.Output.Stdout, " (%s)", model)
	}
	fmt.Fprintln(c.Output.Stdout)
	return 0, reportProvider(c, target.Profile, model)
}

// sessionConfig reports what a provider resolves to, and how to change it.
//
// It deliberately does not run the interactive configuration. That reads stdin
// through a prompter, and the shell a slash command runs in has no dependable
// terminal and a two minute limit, so the honest answer is the command to run in
// a real one.
func sessionConfig(c Context, args []string) (int, error) {
	active := activeSelection(c)
	profile := active.Profile
	if len(args) > 0 {
		profile = profiles.ResolveAlias(strings.TrimSpace(args[0]))
	}

	target, err := profiles.Resolve(profile, c.Catalog, c.Config)
	if err != nil {
		return 2, err
	}

	fmt.Fprintf(c.Output.Stdout, "provider:   %s\n", target.Profile)
	fmt.Fprintf(c.Output.Stdout, "base url:   %s\n", target.BaseURL)
	if target.Model != "" {
		fmt.Fprintf(c.Output.Stdout, "model:      %s\n", target.Model)
	}
	if active.Model != "" && target.Profile == active.Profile {
		fmt.Fprintf(c.Output.Stdout, "model tag:  %s (remembered)\n", active.Model)
	}
	fmt.Fprintf(c.Output.Stdout, "credential: %s\n", credentialSummary(target, c.Secrets))

	fmt.Fprintln(c.Output.Stdout)
	fmt.Fprintln(c.Output.Stdout, "To change these, run this in a terminal:")
	fmt.Fprintf(c.Output.Stdout, "  clother config %s\n", target.Profile)
	return 0, nil
}

func credentialSummary(target profiles.Target, secrets config.Secrets) string {
	switch target.AuthMode {
	case providers.AuthSecret:
		if strings.TrimSpace(secrets[target.SecretKey]) == "" {
			return target.SecretKey + " is NOT set"
		}
		return target.SecretKey + " is set"
	default:
		return "none needed"
	}
}

// reportProvider says which provider the running session is on and how to
// continue it under the remembered one.
//
// Switching a running session is impossible: Claude Code read the base URL and
// the token at startup, and a subprocess cannot change its parent's
// environment. So the helper's real job is to make the restart one paste.
func reportProvider(c Context, profile, model string) error {
	current := sessionProfile()
	switch {
	case current == "":
		fmt.Fprintln(c.Output.Stdout, "this session's provider is not recorded in its environment")
	case current == profile:
		fmt.Fprintf(c.Output.Stdout, "this session is already running on: %s\n", current)
	default:
		fmt.Fprintf(c.Output.Stdout, "this session is still running on: %s (switching needs a restart)\n", current)
	}

	fmt.Fprintf(c.Output.Stdout, "to continue it on %s:\n", profile)
	fmt.Fprintf(c.Output.Stdout, "  %s\n", resumeCommand(profile, model))
	return nil
}

// resumeCommand is the line that continues the current session under the chosen
// provider.
func resumeCommand(profile, model string) string {
	parts := []string{"clother", profile}
	if model != "" {
		parts = append(parts, "--model", model)
	}
	if id := sessionID(); id != "" {
		parts = append(parts, "--resume", id)
	} else {
		// Claude Code exports the id into the environment of every tool it runs.
		// Without it, --continue is the honest fallback rather than a session id
		// guessed from a directory listing: resuming the wrong conversation is a
		// worse answer than resuming none.
		parts = append(parts, "--continue")
	}
	return strings.Join(parts, " ")
}

// activeSelection is what a bare `clother` would launch right now.
func activeSelection(c Context) config.Active {
	if c.Config != nil && c.Config.Active != nil {
		return *c.Config.Active
	}
	return config.Active{Profile: profiles.DefaultProfile}
}

// sessionProfile is the provider the running session was launched under, as the
// launcher left it in the environment.
func sessionProfile() string {
	return strings.TrimSpace(os.Getenv("CLOTHER_PROFILE"))
}

// sessionID is the id that continues the running session.
func sessionID() string {
	return strings.TrimSpace(os.Getenv("CLAUDE_CODE_SESSION_ID"))
}
