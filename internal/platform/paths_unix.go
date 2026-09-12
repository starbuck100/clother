//go:build !windows

package platform

import (
	"os"
	"path/filepath"
	"runtime"
)

// DefaultBinDir is the fallback bin directory when no claude binary was found
// on PATH. It matches the historical Clother choice on each platform.
func DefaultBinDir(home string) string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "bin")
	}
	return filepath.Join(home, ".local", "bin")
}

// DefaultConfigDir follows the XDG base directory spec.
func DefaultConfigDir(home string) string {
	return filepath.Join(envOr("XDG_CONFIG_HOME", filepath.Join(home, ".config")), "clother")
}

// DefaultDataDir follows the XDG base directory spec.
func DefaultDataDir(home string) string {
	return filepath.Join(envOr("XDG_DATA_HOME", filepath.Join(home, ".local", "share")), "clother")
}

// DefaultCacheDir follows the XDG base directory spec.
func DefaultCacheDir(home string) string {
	return filepath.Join(envOr("XDG_CACHE_HOME", filepath.Join(home, ".cache")), "clother")
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
