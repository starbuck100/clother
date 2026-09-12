//go:build !windows

package platform

// envCaseSensitive reports whether environment variable names distinguish
// case. They do on Unix, so a prefix test there is an exact one.
const envCaseSensitive = true
