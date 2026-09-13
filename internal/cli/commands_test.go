package cli

import (
	"slices"
	"testing"
)

func TestIsCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want bool
		in   string
	}{
		{
			name: "plain command",
			in:   "list",
			want: true,
		},
		{
			// Hidden from help is not the same as absent: forwarding the helper
			// to Claude Code as a prompt would be a bug.
			name: "hidden command is still a command",
			in:   "__session",
			want: true,
		},
		{
			name: "help is a command",
			in:   "help",
			want: true,
		},
		{
			name: "provider name is not a command",
			in:   "zai",
			want: false,
		},
		{
			name: "empty string is not a command",
			in:   "",
			want: false,
		},
		{
			name: "option is not a command",
			in:   "--json",
			want: false,
		},
		{
			name: "case matters",
			in:   "List",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := IsCommand(tt.in); got != tt.want {
				t.Errorf("IsCommand(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// The register is handed out as a copy, because a caller that sorted or trimmed
// it in place would change the routing decision made by the next caller.
func TestCommandNamesIsCopied(t *testing.T) {
	t.Parallel()

	first := CommandNames()
	if len(first) == 0 {
		t.Fatal("CommandNames() returned nothing")
	}
	first[0] = "mutated"

	second := CommandNames()
	if second[0] == "mutated" {
		t.Error("CommandNames() shares its backing slice between calls")
	}
	if !IsCommand("list") {
		t.Error("the register lost a command after a caller mutated its copy")
	}
}

func TestNearMiss(t *testing.T) {
	t.Parallel()

	commands := CommandNames()

	tests := []struct {
		name string
		pool []string
		word string
		want string
		miss bool
	}{
		{
			name: "transposed letters of a command",
			pool: commands,
			word: "hlep",
			want: "help",
			miss: true,
		},
		{
			name: "missing letter of a command",
			pool: commands,
			word: "lst",
			want: "list",
			miss: true,
		},
		{
			name: "transposed tail of a command",
			pool: commands,
			word: "statsu",
			want: "status",
			miss: true,
		},
		{
			name: "wrong case still finds the command",
			pool: commands,
			word: "Help",
			want: "help",
			miss: true,
		},
		{
			// A provider name is not close to any command, and the router checks
			// profiles before it asks about typos.
			name: "provider name is not close to a command",
			pool: commands,
			word: "zai",
			want: "",
			miss: false,
		},
		{
			// The trade-off of the tight threshold, spelled out: a one-word
			// prompt one edit from a command is refused as a typo, and the
			// refusal message carries the escape hatch.
			name: "one-word prompt one edit from a command is a near miss",
			pool: commands,
			word: "tests",
			want: "test",
			miss: true,
		},
		{
			name: "multi-word prompt is never a near miss",
			pool: commands,
			word: "fix the bug",
			want: "",
			miss: false,
		},
		{
			name: "long prompt is never a near miss",
			pool: commands,
			word: "why is the build failing on windows",
			want: "",
			miss: false,
		},
		{
			name: "profile pool finds a mistyped profile",
			pool: []string{"native", "zai", "openrouter"},
			word: "nativ",
			want: "native",
			miss: true,
		},
		{
			name: "profile pool clears a prompt",
			pool: []string{"native", "zai", "openrouter"},
			word: "summarise the diff",
			want: "",
			miss: false,
		},
		{
			name: "option is never a near miss",
			pool: commands,
			word: "--json",
			want: "",
			miss: false,
		},
		{
			name: "empty word is never a near miss",
			pool: commands,
			word: "",
			want: "",
			miss: false,
		},
		{
			name: "empty pool finds nothing",
			pool: nil,
			word: "hlep",
			want: "",
			miss: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, miss := NearMiss(tt.word, tt.pool)
			if got != tt.want || miss != tt.miss {
				t.Errorf("NearMiss(%q) = (%q, %v), want (%q, %v)", tt.word, got, miss, tt.want, tt.miss)
			}
		})
	}
}

// editDistance is capped on purpose: the caller compares against the limit, so
// anything past it only has to be reported as "past it", and a candidate whose
// length alone rules it out never needs a matrix.
func TestEditDistance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		left  string
		right string
		limit int
		want  int
	}{
		{"", "", 2, 0},
		{"a", "a", 2, 0},
		{"help", "help", 0, 0},
		{"a", "b", 2, 1},
		{"list", "lst", 2, 1},
		{"status", "statsu", 2, 2},
		{"help", "hlep", 2, 2},
		{"", "ab", 2, 2},
		{"", "abc", 2, 3},
		{"a", "abcdefgh", 2, 3},
		{"kitten", "sitting", 2, 3},
		{"kitten", "sitting", 5, 3},
	}

	for _, tt := range tests {
		name := tt.left + "/" + tt.right
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := editDistance(tt.left, tt.right, tt.limit); got != tt.want {
				t.Errorf("editDistance(%q, %q, %d) = %d, want %d", tt.left, tt.right, tt.limit, got, tt.want)
			}
		})
	}
}

// The distance is symmetric, and a capped result must be reported the same way
// from either side, otherwise the closest candidate depends on argument order.
func TestEditDistanceIsSymmetric(t *testing.T) {
	t.Parallel()

	pairs := [][2]string{
		{"list", "lst"},
		{"help", "hlep"},
		{"status", "statsu"},
		{"kitten", "sitting"},
		{"a", "abcdefgh"},
		{"zai", "native"},
	}

	for _, pair := range pairs {
		forward := editDistance(pair[0], pair[1], maxNearMiss)
		backward := editDistance(pair[1], pair[0], maxNearMiss)
		if forward != backward {
			t.Errorf("editDistance(%q, %q) = %d but editDistance(%q, %q) = %d",
				pair[0], pair[1], forward, pair[1], pair[0], backward)
		}
	}
}

// A sanity check on the candidate sources the router will use: every command
// must be reachable by IsCommand, and the hidden helper must be in the register.
func TestCommandNamesCoverTheRegister(t *testing.T) {
	t.Parallel()

	names := CommandNames()
	if !slices.Contains(names, "__session") {
		t.Error("the hidden __session command is missing from the register")
	}
	for _, name := range names {
		if !IsCommand(name) {
			t.Errorf("CommandNames() lists %q but IsCommand(%q) is false", name, name)
		}
	}
}
