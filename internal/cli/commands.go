package cli

import "strings"

// commandNames lists clother's own subcommands, including the hidden __session
// helper. Hidden is not the same as absent: `clother __session ...` is a command
// and must never be mistaken for something to forward to Claude Code.
var commandNames = []string{
	"bench",
	"config",
	"help",
	"info",
	"install",
	"list",
	"status",
	"test",
	"uninstall",
	"update",
	"__session",
}

// CommandNames returns clother's subcommand names. The slice is copied so a
// caller cannot reorder the register the router reads.
func CommandNames() []string {
	out := make([]string, len(commandNames))
	copy(out, commandNames)
	return out
}

// IsCommand reports whether name is one of clother's own subcommands.
func IsCommand(name string) bool {
	for _, command := range commandNames {
		if name == command {
			return true
		}
	}
	return false
}

// maxNearMiss is how many single-character edits still count as a typo.
const maxNearMiss = 2

// NearMiss returns the candidate closest to name when it is within a couple of
// edits of it, so a mistyped command can be reported by name instead of being
// forwarded to Claude Code as a prompt.
//
// The threshold is deliberately tight and the trade-off is real. A one-word
// prompt such as "tests" is one edit from the "test" command and will be refused
// as a typo. That is the price of catching "lst", "hlep" and "statsu", all of
// which would otherwise open a Claude Code session whose prompt is gibberish.
// The refusal carries the escape hatch, and a multi-word prompt is never within
// two edits of a single candidate, so the common case is untouched.
func NearMiss(name string, candidates []string) (string, bool) {
	if name == "" || strings.HasPrefix(name, "-") {
		return "", false
	}

	lowered := strings.ToLower(name)
	best := ""
	bestDistance := maxNearMiss + 1
	for _, candidate := range candidates {
		distance := editDistance(lowered, strings.ToLower(candidate), maxNearMiss)
		if distance < bestDistance {
			bestDistance = distance
			best = candidate
		}
	}
	if bestDistance > maxNearMiss {
		return "", false
	}
	return best, true
}

// editDistance is the Levenshtein distance between a and b, capped at limit. It
// returns limit+1 as soon as the distance is known to exceed the limit, because
// the only caller compares against the limit and would otherwise pay for a full
// matrix per candidate.
func editDistance(a, b string, limit int) int {
	if a == b {
		return 0
	}
	ar, br := []rune(a), []rune(b)
	if diff := len(ar) - len(br); diff > limit || diff < -limit {
		return limit + 1
	}

	// Two rows rather than a full matrix: only the previous row is ever read.
	previous := make([]int, len(br)+1)
	current := make([]int, len(br)+1)
	for j := range previous {
		previous[j] = j
	}

	for i := 1; i <= len(ar); i++ {
		current[0] = i
		rowMin := current[0]
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
			if current[j] < rowMin {
				rowMin = current[j]
			}
		}
		if rowMin > limit {
			return limit + 1
		}
		copy(previous, current)
	}
	return previous[len(br)]
}
