package profiles

import (
	"fmt"
	"strings"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/providers"
)

// DefaultProfile is the Claude subscription. It is the only provider that needs
// no configuration whatsoever, which makes it the only safe answer for a first
// run and the only safe fallback when a remembered name stops resolving.
const DefaultProfile = "native"

// aliases maps the spellings a user is likely to reach for onto catalog ids.
//
// `claude` is the one that earns its place: it is the first thing a new user
// types, and it is neither a subcommand nor a provider id, so without this the
// router would hand it to Claude Code as a prompt. The list is deliberately
// short — every entry is a name that would otherwise be a plausible mistake.
var aliases = map[string]string{
	"claude":       DefaultProfile,
	"subscription": DefaultProfile,
}

// ResolveAlias maps a user-typed provider name onto a catalog id, returning the
// token unchanged when it is not an alias. It is exported so the routing layer
// and the session helper cannot disagree about a spelling.
//
// Whitespace is trimmed because a name that reached us through a shell or a
// command file can carry it. Case is folded only for the alias lookup and not
// for the name itself: every provider id, OpenRouter alias and custom provider
// name is lowercase by construction, so a name that is not lowercase is not a
// provider, and it should be reported as unknown rather than quietly lowercased
// into something that happens to exist.
func ResolveAlias(name string) string {
	trimmed := strings.TrimSpace(name)
	if canonical, ok := aliases[strings.ToLower(trimmed)]; ok {
		return canonical
	}
	return trimmed
}

// Selection is what a bare `clother` invocation resolves to.
type Selection struct {
	Target Target
	// Model is the remembered model tag, to be passed on as --model. It is
	// empty when nothing was remembered, and also when the remembered provider
	// had to be replaced by the fallback: a model tag belongs to the provider it
	// was chosen for, and carrying it across would apply someone else's model to
	// Claude Code.
	Model string
	// Note explains a substitution in one line, and is empty when the remembered
	// provider was used as remembered.
	Note string
}

// Active resolves the provider the bare `clother` invocation should launch.
//
// A remembered name that no longer resolves — a custom provider the user has
// since deleted, an alias they removed — falls back to the default with a Note
// rather than failing. Refusing to launch would leave the user with no obvious
// way back in, and the state that caused it is exactly the state they need a
// working `clother` to repair.
func Active(catalog providers.Catalog, cfg *config.File) (Selection, error) {
	remembered := ""
	if cfg != nil && cfg.Active != nil {
		remembered = strings.TrimSpace(cfg.Active.Profile)
	}

	if remembered == "" {
		target, err := Resolve(DefaultProfile, catalog, cfg)
		if err != nil {
			return Selection{}, err
		}
		return Selection{Target: target}, nil
	}

	target, err := Resolve(ResolveAlias(remembered), catalog, cfg)
	if err == nil {
		return Selection{Target: target, Model: rememberedModel(cfg)}, nil
	}

	fallback, fallbackErr := Resolve(DefaultProfile, catalog, cfg)
	if fallbackErr != nil {
		return Selection{}, fallbackErr
	}
	return Selection{
		Target: fallback,
		Note:   fmt.Sprintf("%q is no longer a configured provider; using %s", remembered, DefaultProfile),
	}, nil
}

func rememberedModel(cfg *config.File) string {
	if cfg == nil || cfg.Active == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Active.Model)
}
