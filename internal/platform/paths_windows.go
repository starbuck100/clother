//go:build windows

package platform

import (
	"os"
	"path/filepath"
)

// DefaultBinDir is the fallback bin directory when no claude binary was found
// on PATH. Windows has no XDG convention and %USERPROFILE%\.local\bin is not a
// location anything else uses, so this follows the per-user program convention
// instead: %LOCALAPPDATA%\Programs\clother\bin.
func DefaultBinDir(home string) string {
	base, err := os.UserCacheDir() // %LOCALAPPDATA% on Windows
	if err != nil || base == "" {
		base = filepath.Join(home, "AppData", "Local")
	}
	return filepath.Join(base, "Programs", "clother", "bin")
}

// DefaultConfigDir is %APPDATA%\clother.
func DefaultConfigDir(home string) string {
	base, err := os.UserConfigDir() // %APPDATA% on Windows
	if err != nil || base == "" {
		base = filepath.Join(home, "AppData", "Roaming")
	}
	return filepath.Join(base, "clother")
}

// DefaultDataDir is %LOCALAPPDATA%\clother. The secrets file lives here, so it
// stays out of the roaming profile.
func DefaultDataDir(home string) string {
	base, err := os.UserCacheDir() // %LOCALAPPDATA% on Windows
	if err != nil || base == "" {
		base = filepath.Join(home, "AppData", "Local")
	}
	return filepath.Join(base, "clother")
}

// DefaultCacheDir is %LOCALAPPDATA%\clother\cache.
func DefaultCacheDir(home string) string {
	return filepath.Join(DefaultDataDir(home), "cache")
}
