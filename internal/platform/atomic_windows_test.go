//go:build windows

package platform

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// lockFile opens path denying every kind of sharing. That is exactly what a
// virus scanner or the search indexer does while it reads a file that was just
// written, and it is what turns a rename into ERROR_SHARING_VIOLATION.
func lockFile(t *testing.T, path string) syscall.Handle {
	t.Helper()
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := syscall.CreateFile(ptr, syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	return handle
}

func TestAtomicWriteWaitsOutATransientLock(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state.json")
	if err := os.WriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Released well before the third attempt at ~200ms, so the retry schedule
	// is what carries the write through — a single attempt would fail here.
	handle := lockFile(t, path)
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = syscall.CloseHandle(handle)
	}()

	if err := AtomicWrite(path, []byte("second"), 0o644); err != nil {
		t.Fatalf("AtomicWrite() = %v; a lock that clears in 100ms must be waited out", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second" {
		t.Fatalf("content = %q, want second", string(got))
	}
}

func TestAtomicWriteFailsCleanlyOnAHardLock(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state.json")
	if err := os.WriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}

	handle := lockFile(t, path)
	err := AtomicWrite(path, []byte("second"), 0o644)
	_ = syscall.CloseHandle(handle)

	if err == nil {
		t.Fatal("AtomicWrite() succeeded against a lock that never clears")
	}
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		t.Fatalf("AtomicWrite() = %v, want a Windows errno so callers can classify it", err)
	}

	// The failed write must leave the previous content in place rather than a
	// truncated or half-replaced file.
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "first" {
		t.Fatalf("content = %q after a failed write, want the original first", string(got))
	}
}
