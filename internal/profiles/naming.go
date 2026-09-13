package profiles

import "regexp"

// providerNamePattern is the alphabet of every provider name clother can have
// created: catalog ids, OpenRouter aliases and custom providers are all built
// from it. It is defined once and shared with the interactive configuration,
// because a name the configuration would refuse must not be one the launcher
// accepts, and the other way round.
var providerNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// IsProviderName reports whether name has the shape of a provider name. It says
// nothing about whether such a provider exists — that is what resolution is for.
//
// No length limit applies. A name only reaches stored state after it has
// resolved, and resolution is the gate, so a bound here would only ever reject
// names that already exist.
func IsProviderName(name string) bool {
	return providerNamePattern.MatchString(name)
}

// MaxModelTagLen bounds a model tag. Unlike a provider name, a tag is accepted
// without being resolved against any list of known models, so it lands in the
// configuration verbatim and needs a ceiling of its own.
const MaxModelTagLen = 200

// modelTagPattern matches a vendor-qualified model tag such as
// "moonshotai/kimi-k2.6". The vendor part is the restricted one; the model part
// additionally allows the colon OpenRouter uses for variants (":free",
// ":batch") and the leading tilde of its rolling aliases ("~vendor/latest").
var modelTagPattern = regexp.MustCompile(`^~?[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._:-]*$`)

// IsModelTag reports whether tag has the shape of a model tag for a provider
// that takes an arbitrary one.
//
// The strictness is the point. This value is taken straight off a command line,
// handed to Claude Code as a model name and stored in the configuration, so
// anything carrying whitespace or a shell metacharacter has to be refused here
// rather than relied upon to be harmless further down.
func IsModelTag(tag string) bool {
	if tag == "" || len(tag) > MaxModelTagLen {
		return false
	}
	return modelTagPattern.MatchString(tag)
}
