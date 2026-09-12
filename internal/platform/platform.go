// Package platform isolates the operating-system differences that matter to
// Clother: how launchers are linked, how files are replaced atomically, where
// the default directories live, and how a secret is read without echoing it.
//
// Every function here has a Unix and a Windows implementation. The Unix side
// reproduces the historical behaviour exactly (relative and absolute symlinks),
// so the Unix build does not change; the Windows side uses NTFS hardlinks,
// junctions and reparse-point-free fallbacks, none of which require the
// SeCreateSymbolicLinkPrivilege that plain symlinks demand.
package platform

import (
	"os"
	"path/filepath"
	"strings"
)

// HasEnvPrefix reports whether an environment variable name begins with prefix,
// honouring the platform's case rules for variable names — exact on Unix, folded
// on Windows, where the two spellings are the same variable.
func HasEnvPrefix(key, prefix string) bool {
	if len(key) < len(prefix) {
		return false
	}
	if envCaseSensitive {
		return key[:len(prefix)] == prefix
	}
	return strings.EqualFold(key[:len(prefix)], prefix)
}

// SameFile reports whether two paths denote the same underlying file.
//
// This is the load-bearing check for launcher self-detection. os.SameFile
// compares device and inode on Unix, and volume serial plus file index on
// Windows, so it sees through symlinks *and* hardlinks — which plain path
// comparison cannot, and which filepath.EvalSymlinks only manages for
// symlinks. Without it a hardlinked `claude` shim would look like a foreign
// binary and re-exec itself forever.
func SameFile(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	infoA, errA := os.Stat(a)
	infoB, errB := os.Stat(b)
	if errA != nil || errB != nil {
		return false
	}
	return os.SameFile(infoA, infoB)
}

// shimSizeFloor is the smallest file the size tiebreak will consider. A real
// Claude Code install is tens of megabytes; anything below this is not worth
// guessing about.
const shimSizeFloor = 1 << 20

// SameInstallation reports whether b is another copy of the same clother
// installation as a.
//
// SameFile answers this for symlinks and hardlinks, but not for a genuine copy:
// a copy is a distinct file with its own index, so file identity cannot see it.
// On Windows the `claude` shim *is* a copy — deliberately, so that an in-place
// rewrite of claude.exe cannot destroy clother.exe — which is exactly the case
// that would otherwise send the shim into infinite self-execution. There the
// file size is used as a tiebreak: clother never shares its exact byte size
// with a real Claude Code binary. On Unix file identity is always sufficient,
// so the tiebreak stays off and no false positive is possible.
func SameInstallation(a, b string) bool {
	if SameFile(a, b) {
		return true
	}
	if !sizeTiebreak {
		return false
	}
	infoA, errA := os.Stat(a)
	infoB, errB := os.Stat(b)
	if errA != nil || errB != nil || infoA.IsDir() || infoB.IsDir() {
		return false
	}
	size := infoA.Size()
	return size == infoB.Size() && size >= shimSizeFloor
}

// exeSuffix is the extension appended to launcher names on the current
// platform. It is empty everywhere but Windows.
const exeSuffix = exeExt

// LauncherName returns the on-disk name of the launcher for a profile.
func LauncherName(profile string) string {
	return "clother-" + profile + exeSuffix
}

// BinaryName returns the on-disk name of the main clother binary.
func BinaryName() string {
	return "clother" + exeSuffix
}

// RealClaudeName returns the on-disk name the real Claude binary is preserved
// under inside the bin directory.
func RealClaudeName() string {
	return "claude-real" + exeSuffix
}

// ClaudeName returns the on-disk name of the `claude` shim.
func ClaudeName() string {
	return "claude" + exeSuffix
}

// InvocationName reduces argv[0] to the command name the program was invoked
// under: no directory, and no platform executable extension.
//
// The profile a launcher runs as is read from this name, so getting it wrong is
// not cosmetic — on Windows the raw base name of `clother-zai.exe` would parse
// as the profile "zai.exe", which resolves to nothing.
func InvocationName(argv0 string) string {
	base := filepath.Base(argv0)
	if base == "" || base == "." || base == string(filepath.Separator) {
		// Some Windows spawners pass an empty argv[0]; the image the process
		// was actually started from is the next best answer.
		if exe, err := os.Executable(); err == nil {
			base = filepath.Base(exe)
		}
	}
	for _, ext := range invocationExtensions {
		if len(base) > len(ext) && strings.EqualFold(base[len(base)-len(ext):], ext) {
			return base[:len(base)-len(ext)]
		}
	}
	return base
}

// IsClaudeName reports whether argv[0] names the `claude` shim.
func IsClaudeName(argv0 string) bool {
	return InvocationName(argv0) == "claude"
}

// CopyFile writes a copy of src at dst through an atomic replace.
//
// Copying onto itself is a no-op rather than a truncation: `clother install`
// routinely runs from the very file it would write.
func CopyFile(src, dst string) error {
	if SameFile(src, dst) {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return AtomicWrite(dst, data, 0o755)
}

// siblingPath returns the absolute path of the file named name inside the same
// directory as link.
func siblingPath(name, link string) string {
	return filepath.Join(filepath.Dir(link), name)
}
