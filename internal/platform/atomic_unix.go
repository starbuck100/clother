//go:build !windows

package platform

import (
	"os"
	"path/filepath"
)

// AtomicWrite writes data to path through a temporary file in the same
// directory followed by a rename, so a concurrent reader never observes a
// partially written file.
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
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// RemoveExecutable deletes an executable, reporting whether the name is now
// free. Unix unlinks a running image like any other file, so there is nothing
// to work around.
func RemoveExecutable(path string) (bool, error) {
	err := os.Remove(path)
	if err == nil || os.IsNotExist(err) {
		return true, nil
	}
	return false, err
}
