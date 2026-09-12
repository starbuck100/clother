//go:build !windows

package session

// resolveProjectDir returns the session directory for a working directory. On
// Unix the derived name is used as-is: two paths differing only in case are two
// different projects, and the filesystem says so.
func resolveProjectDir(root, cwd string) string {
	return ProjectDir(root, cwd)
}
