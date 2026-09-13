package cli

import (
	"reflect"
	"slices"
	"testing"
)

// consumedNames is how specs are compared: an OptionSpec carries a func, and
// func values are never equal under reflect.DeepEqual. The canonical name is the
// identity that matters to a caller anyway, because that is what it reports.
func consumedNames(split Split) []string {
	names := make([]string, 0, len(split.Consumed))
	for _, spec := range split.Consumed {
		names = append(names, spec.Name)
	}
	return names
}

func TestSplitClotherOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		rest []string
		want Options
	}{
		{
			name: "no arguments",
			args: nil,
			rest: nil,
			want: Options{},
		},
		{
			name: "clother-only option is consumed",
			args: []string{"--json"},
			rest: nil,
			want: Options{Format: "json"},
		},
		{
			name: "clother-only short option is consumed",
			args: []string{"-q"},
			rest: nil,
			want: Options{Quiet: true},
		},
		{
			name: "version is clother's alone",
			args: []string{"-V"},
			rest: nil,
			want: Options{Version: true},
		},
		{
			// Shared with Claude Code, so it must reach Claude Code. Consuming
			// it here would change what `clother --verbose` does today.
			name: "shared option stays in rest",
			args: []string{"--verbose"},
			rest: []string{"--verbose"},
			want: Options{},
		},
		{
			name: "shared short option stays in rest",
			args: []string{"-h"},
			rest: []string{"-h"},
			want: Options{},
		},
		{
			name: "shared debug stays in rest",
			args: []string{"--debug"},
			rest: []string{"--debug"},
			want: Options{},
		},
		{
			name: "unknown option stays in rest",
			args: []string{"--bogus"},
			rest: []string{"--bogus"},
			want: Options{},
		},
		{
			name: "bin-dir consumes the next argument",
			args: []string{"--bin-dir", "/tmp/bin"},
			rest: nil,
			want: Options{BinDir: "/tmp/bin"},
		},
		{
			// Mirrors Parse, which also takes the next token unconditionally.
			name: "bin-dir takes the next token even when it looks like an option",
			args: []string{"--bin-dir", "--json"},
			rest: nil,
			want: Options{BinDir: "--json"},
		},
		{
			name: "end of options is retained with its tail",
			args: []string{"--", "--json"},
			rest: []string{"--", "--json"},
			want: Options{},
		},
		{
			name: "end of options alone is retained",
			args: []string{"--"},
			rest: []string{"--"},
			want: Options{},
		},
		{
			// A launcher invocation: one claude option, one clother option, one
			// provider name all in a row.
			name: "launcher arguments with a clother option mixed in",
			args: []string{"--yolo", "--bin-dir", "/tmp/bin", "zai"},
			rest: []string{"--yolo", "zai"},
			want: Options{BinDir: "/tmp/bin"},
		},
		{
			name: "consumed options are recorded in order",
			args: []string{"--quiet", "--json"},
			rest: nil,
			want: Options{Quiet: true, Format: "json"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			split, err := SplitClotherOptions(tt.args)
			if err != nil {
				t.Fatalf("SplitClotherOptions(%q) error = %v, want nil", tt.args, err)
			}
			if !reflect.DeepEqual(split.Options, tt.want) {
				t.Errorf("SplitClotherOptions(%q) Options = %#v, want %#v", tt.args, split.Options, tt.want)
			}
			if !slices.Equal(split.Rest, tt.rest) {
				t.Errorf("SplitClotherOptions(%q) Rest = %q, want %q", tt.args, split.Rest, tt.rest)
			}
		})
	}
}

func TestSplitClotherOptionsConsumed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "nothing consumed",
			args: []string{"zai", "--yolo"},
			want: []string{},
		},
		{
			name: "canonical name is recorded for a short spelling",
			args: []string{"-q"},
			want: []string{"--quiet"},
		},
		{
			name: "both consumed, in order",
			args: []string{"--json", "-y"},
			want: []string{"--json", "--yes"},
		},
		{
			name: "value-taking option is recorded",
			args: []string{"--bin-dir", "/tmp/bin"},
			want: []string{"--bin-dir"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			split, err := SplitClotherOptions(tt.args)
			if err != nil {
				t.Fatalf("SplitClotherOptions(%q) error = %v, want nil", tt.args, err)
			}
			if got := consumedNames(split); !slices.Equal(got, tt.want) {
				t.Errorf("SplitClotherOptions(%q) consumed = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestSplitClotherOptionsErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "bin-dir without a value",
			args: []string{"--bin-dir"},
			want: "--bin-dir requires a path",
		},
		{
			name: "bin-dir without a value after other arguments",
			args: []string{"zai", "--bin-dir"},
			want: "--bin-dir requires a path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := SplitClotherOptions(tt.args)
			if err == nil {
				t.Fatalf("SplitClotherOptions(%q) error = nil, want %q", tt.args, tt.want)
			}
			if err.Error() != tt.want {
				t.Errorf("SplitClotherOptions(%q) error = %q, want %q", tt.args, err, tt.want)
			}
		})
	}
}

// Refused is what stops a launch from silently dropping a flag the user typed.
// Only options that have no meaning when clother is not the one running see
// their command count matter.
func TestSplitRefused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
		miss bool
	}{
		{
			name: "json is refused in launch mode",
			args: []string{"--json"},
			want: "--json",
			miss: true,
		},
		{
			name: "plain is refused in launch mode",
			args: []string{"--plain"},
			want: "--plain",
			miss: true,
		},
		{
			name: "version is refused in launch mode",
			args: []string{"-V"},
			want: "--version",
			miss: true,
		},
		{
			name: "yes is refused in launch mode",
			args: []string{"--yes"},
			want: "--yes",
			miss: true,
		},
		{
			name: "no-shim is refused in launch mode",
			args: []string{"--no-shim"},
			want: "--no-shim",
			miss: true,
		},
		{
			name: "no-input is refused in launch mode",
			args: []string{"--no-input"},
			want: "--no-input",
			miss: true,
		},
		{
			// Meaningful either way: it selects the bin directory the launcher
			// will use, and it only controls clother's own banner.
			name: "bin-dir is allowed in launch mode",
			args: []string{"--bin-dir", "/tmp/bin"},
			want: "",
			miss: false,
		},
		{
			name: "no-banner is allowed in launch mode",
			args: []string{"--no-banner"},
			want: "",
			miss: false,
		},
		{
			name: "quiet is allowed in launch mode",
			args: []string{"-q"},
			want: "",
			miss: false,
		},
		{
			name: "nothing consumed means nothing refused",
			args: []string{"zai"},
			want: "",
			miss: false,
		},
		{
			// The first refusal wins, so the message names one option.
			name: "first refused option is reported",
			args: []string{"--quiet", "--json"},
			want: "--json",
			miss: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			split, err := SplitClotherOptions(tt.args)
			if err != nil {
				t.Fatalf("SplitClotherOptions(%q) error = %v, want nil", tt.args, err)
			}
			got, miss := split.Refused()
			if got != tt.want || miss != tt.miss {
				t.Errorf("SplitClotherOptions(%q) Refused() = (%q, %v), want (%q, %v)", tt.args, got, miss, tt.want, tt.miss)
			}
		})
	}
}

func TestLookup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		arg  string
		want string
		ok   bool
	}{
		{
			name: "canonical long name",
			arg:  "--help",
			want: "--help",
			ok:   true,
		},
		{
			name: "alias resolves to the canonical name",
			arg:  "-h",
			want: "--help",
			ok:   true,
		},
		{
			name: "version alias does not collide with verbose",
			arg:  "-V",
			want: "--version",
			ok:   true,
		},
		{
			name: "verbose alias does not collide with version",
			arg:  "-v",
			want: "--verbose",
			ok:   true,
		},
		{
			name: "debug alias",
			arg:  "-d",
			want: "--debug",
			ok:   true,
		},
		{
			name: "value-taking option",
			arg:  "--bin-dir",
			want: "--bin-dir",
			ok:   true,
		},
		{
			name: "unknown option",
			arg:  "--bogus",
			want: "",
			ok:   false,
		},
		{
			name: "positional is not an option",
			arg:  "zai",
			want: "",
			ok:   false,
		},
		{
			name: "empty string is not an option",
			arg:  "",
			want: "",
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			spec, ok := Lookup(tt.arg)
			if ok != tt.ok {
				t.Fatalf("Lookup(%q) ok = %v, want %v", tt.arg, ok, tt.ok)
			}
			if ok && spec.Name != tt.want {
				t.Errorf("Lookup(%q) Name = %q, want %q", tt.arg, spec.Name, tt.want)
			}
		})
	}
}

// The table is rebuilt per call so that one caller cannot change what the next
// one parses. Specs returns a fresh slice, and every TakesValue spec must name
// what it consumes, because the missing-value error interpolates it.
func TestSpecsIsIndependent(t *testing.T) {
	t.Parallel()

	first := Specs()
	if len(first) == 0 {
		t.Fatal("Specs() returned no options")
	}
	first[0].Name = "--mutated"

	second := Specs()
	if second[0].Name == "--mutated" {
		t.Error("Specs() shares its backing table between calls")
	}

	for _, spec := range second {
		if spec.TakesValue && spec.ValueHint == "" {
			t.Errorf("spec %q takes a value but has no ValueHint for the missing-value error", spec.Name)
		}
		if spec.Apply == nil {
			t.Errorf("spec %q has no Apply function", spec.Name)
		}
	}
}
