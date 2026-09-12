//go:build windows

package runtime

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jolehuit/clother/internal/platform"
)

const (
	// overlayContainer holds every overlay and lives beside the config
	// directory, which is what guarantees the same volume (see createOverlayDir).
	overlayContainer = ".clother-overlays"
	overlayPrefix    = "overlay-"
	// overlayMaxAge is how long debris from a killed run is left alone.
	overlayMaxAge = 24 * time.Hour
	// fileAttributeHidden is FILE_ATTRIBUTE_HIDDEN, unexported in syscall.
	fileAttributeHidden = 0x2
)

// createOverlayDir creates the overlay beside the config directory rather than
// in %TEMP%, and both reasons for that are hard requirements on Windows.
//
// The mirror is built from hardlinks and directory junctions, and both are
// bound to a single volume — %TEMP% may well be on a different one, and then
// every link fails no matter what privileges the user has. Creating the overlay
// inside the config directory's own parent makes same-volume a property of the
// layout rather than a hope.
func createOverlayDir(sourceDir string) (string, error) {
	parent := filepath.Dir(sourceDir)
	sweepStaleOverlays(parent)

	container := filepath.Join(parent, overlayContainer)
	if err := os.MkdirAll(container, 0o700); err != nil {
		return "", err
	}
	markHidden(container)

	name := overlayPrefix + strconv.Itoa(os.Getpid()) + "-" +
		strconv.FormatInt(time.Now().UnixNano(), 36)
	dir := filepath.Join(container, name)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// cleanupOverlay removes the overlay, after rescuing anything it holds the only
// copy of.
func cleanupOverlay(overlayDir string, links []overlayLink) {
	reconcileOverlay(links)

	// Removing each link by name comes first and is not redundant with the
	// RemoveAll below: os.Remove on a junction or symlink deletes the reparse
	// point itself and never descends into the target, so a mirrored directory
	// keeps its contents. RemoveAll is only the backstop for leftovers.
	for _, link := range links {
		_ = os.Remove(link.overlay)
	}
	_ = os.RemoveAll(overlayDir)
	// Shared with any concurrently running clother; removing it fails
	// harmlessly while another overlay is still inside.
	_ = os.Remove(filepath.Dir(overlayDir))
}

// reconcileOverlay rescues files whose newest version lives in the overlay.
//
// Claude Code writes its state atomically — a temporary file, then a rename
// over the target. A rename replaces the name, and if the target was the
// overlay's own path the hardlink is broken: the overlay now holds a new file
// while the original still holds the old content. Deleting the overlay would
// then throw away the session state written during the run. The identity check
// recognises that case, and the modification times decide which side wins so
// that a concurrently updated original is never clobbered by a stale copy.
func reconcileOverlay(links []overlayLink) {
	for _, link := range links {
		overlayInfo, err := os.Lstat(link.overlay)
		if err != nil || overlayInfo.IsDir() || overlayInfo.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if platform.SameFile(link.overlay, link.source) {
			continue // still one file: nothing was replaced
		}
		if sourceInfo, err := os.Stat(link.source); err == nil && !sourceInfo.ModTime().Before(overlayInfo.ModTime()) {
			continue // the original moved on after us; it wins
		}
		_ = os.Rename(link.overlay, link.source)
	}
}

// sweepStaleOverlays removes debris left by a clother that was killed before it
// could clean up. The age gate is the whole guard: an overlay is only touched
// once it is a day old, by which point no live run can still own it, and the
// process id in the name is there for debugging rather than for liveness
// checks, which would need to open a handle on every candidate.
func sweepStaleOverlays(parent string) {
	container := filepath.Join(parent, overlayContainer)
	entries, err := os.ReadDir(container)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-overlayMaxAge)
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), overlayPrefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(container, entry.Name()))
	}
}

// markHidden sets the hidden attribute but keeps whatever the path already had;
// SetFileAttributes replaces the whole attribute set rather than OR-ing into it.
func markHidden(path string) {
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	attrs, err := syscall.GetFileAttributes(ptr)
	if err != nil {
		return
	}
	_ = syscall.SetFileAttributes(ptr, attrs|fileAttributeHidden)
}
