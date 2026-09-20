package runtime

import (
	"os"
	"path/filepath"
	"strings"
)

// The shapes a per-session Claude config overlay can have.
//
// Windows builds them inside a container beside the config directory, because
// hardlinks and junctions cannot cross a filesystem. Unix builds them in the
// system temp directory, where symlinks cross freely. Both have to be
// recognisable from the path alone, because a clother running inside a session
// sees CLAUDE_CONFIG_DIR pointing at one of them.
//
// These literals repeat what the platform-tagged creators use instead of sharing
// a constant with them, because those files are compiled per platform and this
// one is not. TestOverlayDirRecognisesACreatedOverlay keeps the two in step: it
// builds an overlay with the real creator on whichever platform the test runs on
// and asserts the recogniser accepts it, so a change to either shape fails there.
const (
	overlayContainerName = ".clother-overlays"
	overlayDirPrefix     = "overlay-"
	tempOverlayDirPrefix = "clother-claude-config-"
)

// IsOverlayDir reports whether dir is one of clother's per-session overlays.
func IsOverlayDir(dir string) bool {
	if dir == "" {
		return false
	}

	cleaned := filepath.Clean(dir)
	base := filepath.Base(cleaned)

	if strings.HasPrefix(base, tempOverlayDirPrefix) {
		return true
	}
	if strings.HasPrefix(base, overlayDirPrefix) {
		return filepath.Base(filepath.Dir(cleaned)) == overlayContainerName
	}
	return false
}

// ClaudeConfigDir returns the Claude Code config directory that outlives a
// clother session, or "" when it cannot be determined.
//
// Inside a session clother launched, CLAUDE_CONFIG_DIR is the session's overlay,
// and the overlay is deleted when the session ends — so anything written there
// is deleted with it. An overlay is therefore refused rather than resolved: it
// is built beside the directory it mirrors, and which directory that was cannot
// be recovered from the overlay path. Returning "" makes the caller skip the
// write, which costs nothing; guessing would put user-level files somewhere they
// do not belong.
//
// An empty value is treated as unset. It is not trimmed, because a config
// directory path is a path and trimming one would silently change it.
func ClaudeConfigDir() string {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir != "" {
		if IsOverlayDir(dir) {
			return ""
		}
		return dir
	}

	home := userHomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".claude")
}
