// Package testutil holds the helpers that more than one package's tests need.
// Nothing outside a _test.go file imports it, so it is never linked into a
// binary.
package testutil

import (
	"path/filepath"
	"testing"
)

// SetHome points the notion of "home" at dir for the duration of the test.
//
// Both variables are set on every platform on purpose. os.UserHomeDir reads
// %USERPROFILE% on Windows and $HOME elsewhere, so a test that sets only HOME
// passes on Unix and, on Windows, silently reads the real profile — which for
// this codebase means mirroring and rewriting the developer's own Claude
// configuration. Setting the unused one is inert.
func SetHome(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

// IsolateDirs points every directory clother writes to at a subdirectory of
// root, so a test can never touch the real configuration, cache or bin
// directory of the machine it runs on.
//
// CLOTHER_CONFIG_DIR and friends are set rather than the XDG variables because
// those are what config.LoadPaths reads on both platforms; the XDG trio is set
// as well so a caller that resolves paths itself stays isolated too.
func IsolateDirs(t *testing.T, root string) {
	t.Helper()
	t.Setenv("CLOTHER_CONFIG_DIR", filepath.Join(root, "config"))
	t.Setenv("CLOTHER_DATA_DIR", filepath.Join(root, "data"))
	t.Setenv("CLOTHER_CACHE_DIR", filepath.Join(root, "cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg-config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "xdg-data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "xdg-cache"))
}
