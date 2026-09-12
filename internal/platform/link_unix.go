//go:build !windows

package platform

import (
	"os"
)

// sizeTiebreak is off on Unix: file identity (device and inode) always decides,
// and a size match could only ever produce a false positive.
const sizeTiebreak = false

// exeExt is empty on Unix; launchers carry no extension.
const exeExt = ""

// LinkSibling makes link an alias for the file named name in the same
// directory, using a *relative* symlink so the bin directory stays relocatable.
// This is the historical Clother behaviour, unchanged.
func LinkSibling(name, link string) error {
	return os.Symlink(name, link)
}

// LinkOrCopy makes dst refer to src, preferring a symlink.
func LinkOrCopy(src, dst string) error {
	return os.Symlink(src, dst)
}

// LinkDir makes dst a link to the directory src. Symlinks cover both files and
// directories on Unix, so this is the same call as LinkOrCopy.
func LinkDir(src, dst string) error {
	return os.Symlink(src, dst)
}

// LinkLauncher makes link a launcher for the clother binary at execPath.
//
// absolute chooses between the two layouts clother supports: a sibling
// reference, which keeps the bin directory relocatable, and a direct reference
// to execPath, which a Homebrew-managed binary needs so that `brew upgrade` is
// picked up without re-running install.
func LinkLauncher(execPath, binaryName, link string, absolute bool) error {
	if absolute {
		return LinkOrCopy(execPath, link)
	}
	return LinkSibling(binaryName, link)
}

// LinkShim makes link the `claude` entry that points back at clother. On Unix
// it is the same symlink as any other launcher; on Windows it is a copy, and
// link_windows.go explains why that asymmetry is deliberate.
func LinkShim(execPath, binaryName, link string, absolute bool) error {
	return LinkLauncher(execPath, binaryName, link, absolute)
}
