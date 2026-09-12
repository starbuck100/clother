//go:build windows

package platform

// invocationExtensions are the suffixes a launcher can carry on disk. The
// profile name has to be read back out of `clother-zai.exe`, so these are
// removed before the name is matched. `.ps1` is included even though clother
// never creates one, because a user may well wrap a launcher in one.
var invocationExtensions = []string{".exe", ".cmd", ".bat", ".com", ".ps1"}
