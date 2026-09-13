package profiles

import (
	"fmt"
	"strings"

	"github.com/jolehuit/clother/internal/config"
)

// gatewayKinds are the provider spellings that name a provider indirectly: the
// provider is the argument after them rather than the token itself.
var gatewayKinds = map[string]string{
	"or":     "alias",
	"custom": "provider-name",
}

// IsGateway reports whether kind is one of the indirect provider spellings.
//
// Both are also reachable as their own launcher — clother-or and clother-custom
// — which is how they were reached before a bare `clother` could launch
// anything. This file is the single place that knows how they resolve, so the
// launcher path and the routing path cannot drift apart.
func IsGateway(kind string) bool {
	_, ok := gatewayKinds[kind]
	return ok
}

// GatewayUsageError means a gateway was named without a provider name after it.
// The launcher path prints usage for this instead of an error, so it has to be
// distinguishable from a name that is simply not configured.
type GatewayUsageError struct {
	Usage string
}

func (e *GatewayUsageError) Error() string {
	return e.Usage
}

// Gateway resolves an indirect provider name to the profile it selects, and
// returns the arguments left after it.
//
// `or <alias>` selects an OpenRouter alias and `custom <name>` a configured
// custom provider; both take the name from the next argument. Callers are
// expected to have checked IsGateway first.
func Gateway(kind string, args []string, cfg *config.File) (string, []string, error) {
	argument := gatewayKinds[kind]

	// An option in the name's place is treated as a missing name: `clother or
	// --yolo` was not a request to look up an alias called "--yolo".
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "", nil, &GatewayUsageError{Usage: gatewayUsage(kind, argument)}
	}
	name := args[0]

	switch kind {
	case "or":
		// Resolve validates the alias, so an unconfigured one is reported by
		// name rather than silently becoming a profile that does not exist.
		if cfg == nil || cfg.OpenRouterAliases[name] == "" {
			return "", nil, fmt.Errorf("unknown OpenRouter alias %q — run `clother config openrouter` to configure aliases", name)
		}
		return "or-" + name, args[1:], nil
	case "custom":
		if cfg == nil {
			return "", nil, fmt.Errorf("unknown custom provider %q — run `clother config custom` to configure one", name)
		}
		if _, ok := cfg.CustomProviders[name]; !ok {
			return "", nil, fmt.Errorf("unknown custom provider %q — run `clother config custom` to configure one", name)
		}
		return name, args[1:], nil
	default:
		return "", nil, fmt.Errorf("unknown gateway %q", kind)
	}
}

func gatewayUsage(kind, argument string) string {
	switch kind {
	case "or":
		return "usage: clother-or <alias> [args...]\n\nRun `clother config openrouter` to configure aliases."
	case "custom":
		return "usage: clother-custom <provider-name> [args...]\n\nRun `clother config custom` to configure a custom provider."
	default:
		return fmt.Sprintf("usage: clother %s <%s> [args...]", kind, argument)
	}
}
