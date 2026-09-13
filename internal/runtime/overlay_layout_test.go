package runtime

import (
	"path/filepath"
	"testing"

	"github.com/jolehuit/clother/internal/testutil"
)

// This is the test that keeps the two platform-specific overlay shapes and the
// recogniser in step. It asks the real creator for an overlay on whichever
// platform it runs on — a container beside the config directory on Windows, a
// temp directory on Unix — and requires the recogniser to accept it. Changing
// either creator's naming fails here rather than in the field, where the
// consequence would be writing user-level files into a directory that is
// deleted with the session.
func TestOverlayDirRecognisesACreatedOverlay(t *testing.T) {
	home := t.TempDir()
	source := filepath.Join(home, ".claude")

	overlay, err := createOverlayDir(source)
	if err != nil {
		t.Fatalf("createOverlayDir(%q) error = %v", source, err)
	}
	defer cleanupOverlay(overlay, nil)

	if !IsOverlayDir(overlay) {
		t.Errorf("IsOverlayDir(%q) = false for an overlay clother just created", overlay)
	}
}

func TestIsOverlayDir(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		want bool
	}{
		{
			// Constructed with filepath.Join so the separators are right on the
			// platform running the test; the shape is the same on both.
			name: "windows container shape",
			dir:  filepath.Join("home", "user", overlayContainerName, overlayDirPrefix+"1234"),
			want: true,
		},
		{
			name: "unix temp shape",
			dir:  filepath.Join("tmp", tempOverlayDirPrefix+"1234"),
			want: true,
		},
		{
			name: "container at the root of a relative path",
			dir:  filepath.Join(overlayContainerName, overlayDirPrefix+"1"),
			want: true,
		},
		{
			name: "the config directory itself",
			dir:  filepath.Join("home", "user", ".claude"),
			want: false,
		},
		{
			name: "absolute windows style config directory",
			dir:  `C:\Users\someone\.claude`,
			want: false,
		},
		{
			// The container is not itself an overlay: it holds them.
			name: "the container directory",
			dir:  filepath.Join("home", "user", overlayContainerName),
			want: false,
		},
		{
			// The prefix alone is not enough, the container has to be the parent
			// too. A directory merely named like an overlay is not ours.
			name: "overlay prefix outside the container",
			dir:  filepath.Join("tmp", overlayDirPrefix+"1234"),
			want: false,
		},
		{
			name: "unrelated entry inside the container",
			dir:  filepath.Join("home", "user", overlayContainerName, "notes"),
			want: false,
		},
		{
			// "overlay" without the trailing dash is a different name.
			name: "name that only starts like the prefix",
			dir:  filepath.Join("home", "user", overlayContainerName, "overlay"),
			want: false,
		},
		{
			name: "empty path",
			dir:  "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsOverlayDir(tt.dir); got != tt.want {
				t.Errorf("IsOverlayDir(%q) = %v, want %v", tt.dir, got, tt.want)
			}
		})
	}
}

func TestClaudeConfigDir(t *testing.T) {
	t.Run("unset falls back to the home directory", func(t *testing.T) {
		home := t.TempDir()
		testutil.SetHome(t, home)
		t.Setenv("CLAUDE_CONFIG_DIR", "")

		if got, want := ClaudeConfigDir(), filepath.Join(home, ".claude"); got != want {
			t.Errorf("ClaudeConfigDir() = %q, want %q", got, want)
		}
	})

	t.Run("an explicit directory is used as given", func(t *testing.T) {
		home := t.TempDir()
		testutil.SetHome(t, home)
		explicit := filepath.Join(home, "claude-config")
		t.Setenv("CLAUDE_CONFIG_DIR", explicit)

		if got := ClaudeConfigDir(); got != explicit {
			t.Errorf("ClaudeConfigDir() = %q, want %q", got, explicit)
		}
	})

	t.Run("a session overlay is refused", func(t *testing.T) {
		home := t.TempDir()
		testutil.SetHome(t, home)

		// A real overlay, not a lookalike: this is the case that matters, since
		// writing into it would lose the files when the session ends.
		overlay, err := createOverlayDir(filepath.Join(home, ".claude"))
		if err != nil {
			t.Fatalf("createOverlayDir error = %v", err)
		}
		defer cleanupOverlay(overlay, nil)
		t.Setenv("CLAUDE_CONFIG_DIR", overlay)

		if got := ClaudeConfigDir(); got != "" {
			t.Errorf("ClaudeConfigDir() = %q for an overlay, want an empty refusal", got)
		}
	})

	t.Run("no home and no override yields nothing", func(t *testing.T) {
		testutil.SetHome(t, "")
		t.Setenv("CLAUDE_CONFIG_DIR", "")

		if got := ClaudeConfigDir(); got != "" {
			t.Errorf("ClaudeConfigDir() = %q with no home, want an empty refusal", got)
		}
	})
}
