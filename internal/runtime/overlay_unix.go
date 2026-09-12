//go:build !windows

package runtime

import "os"

// createOverlayDir puts the overlay in the system temp directory, where it has
// always lived. The mirror is built from symlinks, which cross filesystems
// without complaint, so the location costs nothing here.
func createOverlayDir(string) (string, error) {
	return os.MkdirTemp("", "clother-claude-config-*")
}

// cleanupOverlay needs no reconciliation on Unix. An atomic replace of a
// mirrored file replaces the *name* that the symlink points at, so the overlay
// keeps seeing the new content; there is no second copy that could be dropped.
func cleanupOverlay(overlayDir string, _ []overlayLink) {
	_ = os.RemoveAll(overlayDir)
}
