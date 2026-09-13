package commands

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/version"
)

func runStatus(_ context.Context, c Context) (int, error) {
	targets := profiles.All(c.Catalog, c.Config)
	if c.Options.Format == "json" {
		payload := map[string]any{
			"version":  version.Value,
			"config":   c.Paths.ConfigDir,
			"data":     c.Paths.DataDir,
			"bin":      c.Paths.BinDir,
			"profiles": len(targets),
			"active":   activeSummary(c),
		}
		data, _ := json.MarshalIndent(payload, "", "  ")
		fmt.Fprintln(c.Output.Stdout, string(data))
		return 0, nil
	}
	c.Output.Header("Clother Status")
	fmt.Fprintf(c.Output.Stdout, "Version:   %s\n", version.Value)
	fmt.Fprintf(c.Output.Stdout, "Config:    %s\n", c.Paths.ConfigDir)
	fmt.Fprintf(c.Output.Stdout, "Data:      %s\n", c.Paths.DataDir)
	fmt.Fprintf(c.Output.Stdout, "Bin:       %s\n", c.Paths.BinDir)
	fmt.Fprintf(c.Output.Stdout, "Profiles:  %d\n", len(targets))
	// What a bare `clother` would launch. Without this, the remembered choice is
	// invisible, and a user has no way to see why `clother` started what it did.
	fmt.Fprintf(c.Output.Stdout, "Active:    %s\n", activeSummary(c))
	return 0, nil
}

// activeSummary describes the remembered provider in one line, for `status`.
func activeSummary(c Context) string {
	if c.Config == nil || c.Config.Active == nil || c.Config.Active.Profile == "" {
		return "(none yet — the next bare `clother` launches " + profiles.DefaultProfile + ")"
	}
	if model := c.Config.Active.Model; model != "" {
		return c.Config.Active.Profile + " (" + model + ")"
	}
	return c.Config.Active.Profile
}
