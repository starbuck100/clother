//go:build !windows

package platform

// invocationExtensions is empty on Unix: a launcher on PATH is named exactly
// `clother-zai` and never carries a suffix to strip.
var invocationExtensions []string
