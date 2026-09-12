//go:build windows

package platform

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// renameBackoff are the pauses tried between rename attempts. Indexers and
// antivirus scanners routinely hold a handle on a file for a few milliseconds
// after it is written, which turns a plain rename into ERROR_SHARING_VIOLATION.
var renameBackoff = []time.Duration{0, 50 * time.Millisecond, 150 * time.Millisecond, 400 * time.Millisecond}

// Windows error codes. Go's syscall package leaves these unexported, so they
// are spelled out here; they are documented, stable values.
const (
	errAccessDenied     = syscall.Errno(5)
	errSharingViolation = syscall.Errno(32)
	errLockViolation    = syscall.Errno(33)
)

// AtomicWrite writes data to path through a temporary file in the same
// directory followed by a rename, so a concurrent reader never observes a
// partially written file.
//
// On Windows the final rename is the fragile part: MoveFileEx fails when any
// process holds a handle on the destination, and the file currently being
// executed can never be replaced at all. Both cases are handled in replaceFile.
func AtomicWrite(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".clother-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	// os.Chmod on Windows only toggles the read-only attribute; the mode is
	// kept in the signature so callers read the same on both platforms.
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return replaceFile(tmpPath, path)
}

func replaceFile(src, dst string) error {
	var err error
	for _, wait := range renameBackoff {
		if wait > 0 {
			time.Sleep(wait)
		}
		if err = os.Rename(src, dst); err == nil {
			return nil
		}
		if !isTransientLock(err) {
			break
		}
	}

	// The destination could not be replaced in place. Moving the *name* out of
	// the way first often still succeeds, because the lock is usually on the
	// file's contents rather than on its directory entry.
	if _, statErr := os.Lstat(dst); statErr != nil {
		return err
	}
	old := dst + ".old-" + strconv.Itoa(os.Getpid())
	if moveErr := os.Rename(dst, old); moveErr != nil {
		return err
	}
	if err = os.Rename(src, dst); err != nil {
		_ = os.Rename(old, dst)
		return err
	}
	_ = os.Remove(old)
	return nil
}

// RemoveExecutable deletes an executable, reporting whether the name is now
// free.
//
// Deleting a running image is refused on Windows, but renaming one is allowed,
// so a program that removes itself moves the file aside first and then deletes
// it under the new name.
func RemoveExecutable(path string) (bool, error) {
	err := os.Remove(path)
	if err == nil || os.IsNotExist(err) {
		return true, nil
	}
	aside := path + ".old-" + strconv.Itoa(os.Getpid())
	if renameErr := os.Rename(path, aside); renameErr != nil {
		return false, err
	}
	// The name is free either way; failing to delete the moved-aside file only
	// means a leftover that the next install sweeps up.
	_ = os.Remove(aside)
	return true, nil
}

func isTransientLock(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	return errno == errSharingViolation ||
		errno == errLockViolation ||
		errno == errAccessDenied
}
