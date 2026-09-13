package cli

import (
	"reflect"
	"slices"
	"testing"
)

// These tests characterise Parse as it behaves today, before the option table in
// spec.go replaces the switch it currently uses. internal/cli had no test file
// at all, and Parse sits on the path of every existing command, so the current
// behaviour is pinned first and the refactor is measured against it.
//
// One deliberate loosening: a nil Args and an empty Args compare equal. Which of
// the two falls out of the implementation depends on a slice expression and is
// not a contract anyone can rely on, so pinning it would only make a faithful
// refactor look like a regression. Everything else is compared exactly.
func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want Parsed
	}{
		{
			name: "no arguments",
			args: nil,
			want: Parsed{Options: Options{Format: "human"}},
		},
		{
			name: "-h",
			args: []string{"-h"},
			want: Parsed{Options: Options{Help: true, Format: "human"}},
		},
		{
			name: "--help",
			args: []string{"--help"},
			want: Parsed{Options: Options{Help: true, Format: "human"}},
		},
		{
			name: "-V",
			args: []string{"-V"},
			want: Parsed{Options: Options{Version: true, Format: "human"}},
		},
		{
			name: "--version",
			args: []string{"--version"},
			want: Parsed{Options: Options{Version: true, Format: "human"}},
		},
		{
			// -v is verbose, not version. -V is version. Easy to get backwards.
			name: "-v is verbose",
			args: []string{"-v"},
			want: Parsed{Options: Options{Verbose: true, Format: "human"}},
		},
		{
			name: "--verbose",
			args: []string{"--verbose"},
			want: Parsed{Options: Options{Verbose: true, Format: "human"}},
		},
		{
			name: "-d implies verbose",
			args: []string{"-d"},
			want: Parsed{Options: Options{Debug: true, Verbose: true, Format: "human"}},
		},
		{
			name: "--debug implies verbose",
			args: []string{"--debug"},
			want: Parsed{Options: Options{Debug: true, Verbose: true, Format: "human"}},
		},
		{
			name: "-q",
			args: []string{"-q"},
			want: Parsed{Options: Options{Quiet: true, Format: "human"}},
		},
		{
			name: "--quiet",
			args: []string{"--quiet"},
			want: Parsed{Options: Options{Quiet: true, Format: "human"}},
		},
		{
			name: "-y",
			args: []string{"-y"},
			want: Parsed{Options: Options{Yes: true, Format: "human"}},
		},
		{
			name: "--yes",
			args: []string{"--yes"},
			want: Parsed{Options: Options{Yes: true, Format: "human"}},
		},
		{
			name: "--no-input",
			args: []string{"--no-input"},
			want: Parsed{Options: Options{NoInput: true, Format: "human"}},
		},
		{
			name: "--no-banner",
			args: []string{"--no-banner"},
			want: Parsed{Options: Options{NoBanner: true, Format: "human"}},
		},
		{
			name: "--no-shim",
			args: []string{"--no-shim"},
			want: Parsed{Options: Options{NoShim: true, Format: "human"}},
		},
		{
			name: "--json",
			args: []string{"--json"},
			want: Parsed{Options: Options{Format: "json"}},
		},
		{
			name: "--plain",
			args: []string{"--plain"},
			want: Parsed{Options: Options{Format: "plain"}},
		},
		{
			name: "--bin-dir takes the next argument",
			args: []string{"--bin-dir", "/tmp/bin"},
			want: Parsed{Options: Options{BinDir: "/tmp/bin", Format: "human"}},
		},
		{
			// Options are recognised anywhere, not only before the command.
			name: "option between command and argument",
			args: []string{"config", "--bin-dir", "/tmp/bin", "zai"},
			want: Parsed{Options: Options{BinDir: "/tmp/bin", Format: "human"}, Command: "config", Args: []string{"zai"}},
		},
		{
			name: "option after command",
			args: []string{"list", "--json"},
			want: Parsed{Options: Options{Format: "json"}, Command: "list"},
		},
		{
			name: "command without arguments",
			args: []string{"list"},
			want: Parsed{Options: Options{Format: "human"}, Command: "list"},
		},
		{
			name: "command with one argument",
			args: []string{"list", "zai"},
			want: Parsed{Options: Options{Format: "human"}, Command: "list", Args: []string{"zai"}},
		},
		{
			name: "command with two arguments",
			args: []string{"info", "zai", "extra"},
			want: Parsed{Options: Options{Format: "human"}, Command: "info", Args: []string{"zai", "extra"}},
		},
		{
			name: "escape terminator alone",
			args: []string{"--"},
			want: Parsed{Options: Options{Format: "human"}},
		},
		{
			// Everything after -- is positional, so an option-looking token
			// becomes the command name instead of being parsed.
			name: "escape protects an option-looking argument",
			args: []string{"--", "--json"},
			want: Parsed{Options: Options{Format: "human"}, Command: "--json"},
		},
		{
			name: "escape with an argument",
			args: []string{"--", "zai"},
			want: Parsed{Options: Options{Format: "human"}, Command: "zai"},
		},
		{
			// An empty string is not an option and not skipped: it becomes the
			// command name, which Dispatch reads as "no command".
			name: "empty argument becomes the command",
			args: []string{""},
			want: Parsed{Options: Options{Format: "human"}, Command: ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Parse(tt.args)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v, want nil", tt.args, err)
			}
			if !reflect.DeepEqual(got.Options, tt.want.Options) {
				t.Errorf("Parse(%q) Options = %#v, want %#v", tt.args, got.Options, tt.want.Options)
			}
			if got.Command != tt.want.Command {
				t.Errorf("Parse(%q) Command = %q, want %q", tt.args, got.Command, tt.want.Command)
			}
			if !slices.Equal(got.Args, tt.want.Args) {
				t.Errorf("Parse(%q) Args = %q, want %q", tt.args, got.Args, tt.want.Args)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "dash alone",
			args: []string{"-"},
			want: "unknown option -",
		},
		{
			name: "unknown long option",
			args: []string{"--bogus"},
			want: "unknown option --bogus",
		},
		{
			name: "unknown short option",
			args: []string{"-x"},
			want: "unknown option -x",
		},
		{
			name: "bin-dir without a value",
			args: []string{"--bin-dir"},
			want: "--bin-dir requires a path",
		},
		{
			// The value is consumed from the next argument, so a missing value is
			// a missing value no matter where the option sits.
			name: "bin-dir without a value after command and argument",
			args: []string{"config", "zai", "--bin-dir"},
			want: "--bin-dir requires a path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Parse(tt.args)
			if err == nil {
				t.Fatalf("Parse(%q) error = nil, want %q", tt.args, tt.want)
			}
			if err.Error() != tt.want {
				t.Errorf("Parse(%q) error = %q, want %q", tt.args, err, tt.want)
			}
		})
	}
}

// ParseLauncher is the launcher entry point: it understands --no-banner and
// forwards everything else to Claude Code untouched, options included. That
// difference from Parse is the whole point of the function.
func TestParseLauncher(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		args          []string
		wantNoBanner  bool
		wantForwarded []string
	}{
		{
			name:          "no arguments",
			args:          nil,
			wantNoBanner:  false,
			wantForwarded: nil,
		},
		{
			name:          "no-banner alone",
			args:          []string{"--no-banner"},
			wantNoBanner:  true,
			wantForwarded: nil,
		},
		{
			name:          "claude option is forwarded",
			args:          []string{"--yolo"},
			wantNoBanner:  false,
			wantForwarded: []string{"--yolo"},
		},
		{
			name:          "no-banner is stripped wherever it sits",
			args:          []string{"--yolo", "--no-banner", "--resume", "abc"},
			wantNoBanner:  true,
			wantForwarded: []string{"--yolo", "--resume", "abc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			options, forwarded := ParseLauncher(tt.args)
			if options.NoBanner != tt.wantNoBanner {
				t.Errorf("ParseLauncher(%q) NoBanner = %v, want %v", tt.args, options.NoBanner, tt.wantNoBanner)
			}
			if !slices.Equal(forwarded, tt.wantForwarded) {
				t.Errorf("ParseLauncher(%q) forwarded = %q, want %q", tt.args, forwarded, tt.wantForwarded)
			}
		})
	}
}
