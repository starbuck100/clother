package profiles

import (
	"strings"
	"testing"
)

func TestIsProviderName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"catalog id", "zai", true},
		{"catalog id with a dash", "zai-cn", true},
		{"openrouter alias profile", "or-kimi", true},
		{"underscore", "my_provider", true},
		{"single letter", "a", true},
		// The shared alphabet starts at a lowercase letter or a digit, so a
		// leading digit is a valid name. This matches what the interactive
		// configuration has always accepted.
		{"leading digit", "1zai", true},
		{"empty", "", false},
		{"uppercase", "ZAI", false},
		{"leading dash", "-zai", false},
		{"leading underscore", "_zai", false},
		{"dot is not in the alphabet", "zai.exe", false},
		{"slash is not in the alphabet", "zai/other", false},
		{"path traversal", "zai/../etc", false},
		{"space", "zai zai", false},
		{"semicolon", "zai;rm -rf /", false},
		{"backtick", "zai`id`", false},
		{"dollar", "zai$(id)", false},
		{"pipe", "zai|cat", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := IsProviderName(tt.in); got != tt.want {
				t.Errorf("IsProviderName(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// There is no length limit on a provider name, and that is deliberate: a name is
// only stored after it has resolved, so resolution is the gate. Asserting it here
// keeps a future reader from adding a bound that would reject names already on
// disk.
func TestIsProviderNameHasNoLengthLimit(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("a", 300)
	if !IsProviderName(long) {
		t.Errorf("IsProviderName rejected a %d character name", len(long))
	}
}

func TestIsModelTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"the example a user gave", "moonshotai/kimi-k2.6", true},
		{"catalog slug", "anthropic/claude-sonnet-5", true},
		{"vendor with a dash", "z-ai/glm-5.3", true},
		{"variant suffix", "vendor/model:free", true},
		{"batch variant is still a valid tag", "moonshotai/kimi-k2.6:batch", true},
		{"rolling alias", "~z-ai/glm-latest", true},
		{"underscore in the model", "vendor/my_model", true},
		{"no vendor", "kimi-k2.6", false},
		{"empty vendor", "/model", false},
		{"empty model", "vendor/", false},
		{"no separator", "", false},
		{"two separators", "vendor/sub/model", false},
		{"dot leading the model", "vendor/../etc", false},
		{"dot leading the vendor", "../model", false},
		{"trailing newline", "vendor/model\n", false},
		{"space", "vendor/model name", false},
		{"semicolon", "vendor/model;rm -rf /", false},
		{"backtick", "vendor/model`id`", false},
		{"dollar", "vendor/model$(id)", false},
		{"pipe", "vendor/model|cat", false},
		{"quote", "vendor/model\"x", false},
		{"backslash", "vendor\\model", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := IsModelTag(tt.in); got != tt.want {
				t.Errorf("IsModelTag(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// A tag is accepted without being resolved against any list, so it is stored
// verbatim and the ceiling is what stops an argument from becoming the bulk of
// the configuration file.
func TestIsModelTagLengthBoundary(t *testing.T) {
	t.Parallel()

	atLimit := strings.Repeat("a", MaxModelTagLen-2) + "/b"
	if len(atLimit) != MaxModelTagLen {
		t.Fatalf("test is wrong: built %d characters, want %d", len(atLimit), MaxModelTagLen)
	}
	if !IsModelTag(atLimit) {
		t.Errorf("IsModelTag rejected a tag of exactly %d characters", MaxModelTagLen)
	}

	overLimit := strings.Repeat("a", MaxModelTagLen-1) + "/b"
	if len(overLimit) != MaxModelTagLen+1 {
		t.Fatalf("test is wrong: built %d characters, want %d", len(overLimit), MaxModelTagLen+1)
	}
	if IsModelTag(overLimit) {
		t.Errorf("IsModelTag accepted a tag of %d characters", len(overLimit))
	}
}
