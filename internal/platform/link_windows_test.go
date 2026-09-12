//go:build windows

package platform

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLinkDirIsAWorkingJunction covers the whole reason junctions are used: a
// directory symlink needs SeCreateSymbolicLinkPrivilege, a mount-point reparse
// point does not, so this is the only directory link an ordinary user can make.
func TestLinkDirIsAWorkingJunction(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "marker.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(root, "link")
	if err := LinkDir(target, link); err != nil {
		t.Fatalf("LinkDir() = %v", err)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is not reported as a link (mode %v)", link, info.Mode())
	}
	if !SameFile(link, target) {
		t.Fatalf("%s does not resolve to %s", link, target)
	}

	// A write through the link must land in the target. This is what keeps
	// Claude Code's session history out of the overlay: the projects directory
	// is linked, never copied, so nothing written during a run is lost when the
	// overlay is cleaned up.
	if err := os.WriteFile(filepath.Join(link, "through.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "through.txt")); err != nil {
		t.Fatalf("writing through the junction did not reach the target: %v", err)
	}

	// Removing the link removes the reparse point only. If this ever deleted the
	// contents, cleanup would wipe the user's ~/.claude/projects.
	if err := os.Remove(link); err != nil {
		t.Fatalf("os.Remove(junction) = %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "marker.txt")); err != nil {
		t.Fatalf("removing the junction destroyed its target: %v", err)
	}
}

func TestLinkLauncherResolvesToTheBinary(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, BinaryName())
	blob := make([]byte, 2<<20)
	if err := os.WriteFile(binary, blob, 0o755); err != nil {
		t.Fatal(err)
	}

	launcher := filepath.Join(root, LauncherName("zai"))
	if err := LinkLauncher(binary, BinaryName(), launcher, false); err != nil {
		t.Fatalf("LinkLauncher() = %v", err)
	}
	// SameInstallation rather than SameFile: on a filesystem without hardlinks
	// the launcher is a copy, which still runs clother and is still recognised
	// as such. On NTFS — the normal case — it is the same file record.
	if !SameInstallation(launcher, binary) {
		t.Fatalf("%s does not resolve to %s", launcher, binary)
	}
}

// TestLinkShimIsACopyNotAHardlink pins the deliberate difference between the
// shim and every other launcher.
//
// A hardlinked claude.exe would be one file record with clother.exe, so Claude
// Code's own updater rewriting claude.exe in place would rewrite clother with
// it — turning every launcher into stock Claude Code without a word.
func TestLinkShimIsACopyNotAHardlink(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, BinaryName())
	blob := make([]byte, 2<<20)
	copy(blob, "clother")
	if err := os.WriteFile(binary, blob, 0o755); err != nil {
		t.Fatal(err)
	}

	shim := filepath.Join(root, ClaudeName())
	if err := LinkShim(binary, BinaryName(), shim, false); err != nil {
		t.Fatalf("LinkShim() = %v", err)
	}
	if SameFile(shim, binary) {
		t.Fatal("the claude shim shares a file record with clother; an in-place rewrite of one would destroy the other")
	}
	// It must still be recognisable as clother's own binary, or the shim would
	// mistake itself for a real Claude Code and start itself forever.
	if !SameInstallation(shim, binary) {
		t.Fatal("the shim is not recognisable as clother's own binary")
	}

	// Overwriting the shim must leave clother intact — the property the copy
	// exists to guarantee.
	if err := os.WriteFile(shim, []byte("stock claude code"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(blob) {
		t.Fatalf("rewriting the shim changed clother: %d bytes, want %d", len(got), len(blob))
	}
}
