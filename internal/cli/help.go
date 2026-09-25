package cli

import (
	"fmt"
	"io"
	"sort"

	"github.com/jolehuit/clother/internal/providers"
	"github.com/jolehuit/clother/internal/version"
)

func ShowBrief(w io.Writer) {
	fmt.Fprintf(w, "Clother v%s - Multi-provider launcher for Claude CLI\n\n", version.Value)
	fmt.Fprintln(w, "Usage: clother [provider] [claude options]")
	fmt.Fprintln(w, "       clother <command> [args]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "On its own, clother launches Claude Code under the provider you last")
	fmt.Fprintln(w, "chose. `clother zai` switches to Z.AI and remembers it.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  config       Configure a provider")
	fmt.Fprintln(w, "  list         List profiles")
	fmt.Fprintln(w, "  info         Provider details")
	fmt.Fprintln(w, "  test         Test providers")
	fmt.Fprintln(w, "  doctor       Diagnose launchers, credentials, live models; optional tool probe")
	fmt.Fprintln(w, "  bench        Benchmark providers (latency)")
	fmt.Fprintln(w, "  status       Show installation state")
	fmt.Fprintln(w, "  install      Install or repair the launchers and commands")
	fmt.Fprintln(w, "  update       Update to latest version")
	fmt.Fprintln(w, "  uninstall    Remove Clother")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Tip: add --yolo to skip permission prompts.")
	fmt.Fprintln(w, "     clother openrouter --model <vendor>/<model> picks any OpenRouter model.")
	fmt.Fprintln(w, "     After --, everything goes to Claude Code unchanged.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run clother --help for full help.")
}

func ShowFull(w io.Writer, catalog providers.Catalog) {
	fmt.Fprintf(w, "Clother v%s\n", version.Value)
	fmt.Fprintln(w, "Multi-provider launcher for Claude CLI")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  clother [provider] [claude options]")
	fmt.Fprintln(w, "  clother <command> [args]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Launching:")
	fmt.Fprintln(w, "  clother                         the provider you last chose")
	fmt.Fprintln(w, "  clother zai                     switch to Z.AI, and remember it")
	fmt.Fprintln(w, "  clother zai --model glm-4.7     ... with a different model")
	fmt.Fprintln(w, "  clother openrouter <vendor>/<model>   any OpenRouter model")
	fmt.Fprintln(w, "  clother usage                       usage and free quota for both gateways")
	fmt.Fprintln(w, "  clother usage next [provider] [model] compare live free alternatives")
	fmt.Fprintln(w, "  clother doctor kilo [model] probe    real, bounded free tool-call test")
	fmt.Fprintln(w, "  clother config budget 1 1           paid fallback USD limits: UTC day and session")
	fmt.Fprintln(w, "  clother config fallback free|paid    allow only free or configured keyed fallbacks")
	fmt.Fprintln(w, "  clother config kilo                 choose live Kilo free models")
	fmt.Fprintln(w, "  clother-kilo --yolo                  Kilo gateway, no Kilo Code needed")
	fmt.Fprintln(w, "  clother --yolo                  skip permission prompts")
	fmt.Fprintln(w, "  clother -- <args>               everything after -- is Claude Code's")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  A token is a provider name only if it names one; `clother fix the bug`")
	fmt.Fprintln(w, "  passes those words to Claude Code. A model tag is read after the provider")
	fmt.Fprintln(w, "  only for providers whose models are not a fixed list.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Inside Claude Code:")
	fmt.Fprintln(w, "  /clother:provider [name]        show, or switch, the remembered provider")
	fmt.Fprintln(w, "  /clother:config [name]          what a provider resolves to")
	fmt.Fprintln(w, "  /clother:status                 installation state")
	fmt.Fprintln(w, "  /clother:next | :pin | :free | :auto control the live free/fallback router")
	fmt.Fprintln(w, "  /clother:provider changes the saved launch profile and prints the")
	fmt.Fprintln(w, "  command that continues it under the new one.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  config [provider]")
	fmt.Fprintln(w, "  list")
	fmt.Fprintln(w, "  info <provider>")
	fmt.Fprintln(w, "  test [provider]")
	fmt.Fprintln(w, "  bench [provider...] [--prompt \"...\"]")
	fmt.Fprintln(w, "  status")
	fmt.Fprintln(w, "  install")
	fmt.Fprintln(w, "  update")
	fmt.Fprintln(w, "  uninstall")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options:")
	fmt.Fprintln(w, "  -h, --help")
	fmt.Fprintln(w, "  -V, --version")
	fmt.Fprintln(w, "  -v, --verbose")
	fmt.Fprintln(w, "  -d, --debug")
	fmt.Fprintln(w, "  -q, --quiet")
	fmt.Fprintln(w, "  -y, --yes")
	fmt.Fprintln(w, "  --bin-dir <path>")
	fmt.Fprintln(w, "  --no-input")
	fmt.Fprintln(w, "  --no-banner")
	fmt.Fprintln(w, "  --no-shim")
	fmt.Fprintln(w, "  --json")
	fmt.Fprintln(w, "  --plain")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Launcher tips:")
	fmt.Fprintln(w, "  clother-openrouter --yolo --model <vendor>/<model>")
	fmt.Fprintln(w, "  clother-zai --yolo       skip permission prompts")
	fmt.Fprintln(w, "  claude --yolo            same behavior via the Clother shim")
	fmt.Fprintln(w, "  --yolo                   shorthand for --dangerously-skip-permissions")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Providers:")
	for _, category := range catalog.Categories() {
		fmt.Fprintf(w, "  %s\n", category)
		providersInCategory := catalog.ProvidersByCategory(category)
		sort.SliceStable(providersInCategory, func(i, j int) bool {
			return providersInCategory[i].ID < providersInCategory[j].ID
		})
		for _, provider := range providersInCategory {
			fmt.Fprintf(w, "    %-12s %s\n", provider.ID, provider.Description)
		}
	}
	fmt.Fprintln(w)
	// OpenRouter is a catalog provider now and is listed with the others above.
	// The heading says so rather than repeating "Advanced", which is the name of
	// the catalog category it sits in.
	fmt.Fprintln(w, "Beyond the catalog:")
	fmt.Fprintln(w, "    custom       Anthropic-compatible endpoint")
}
