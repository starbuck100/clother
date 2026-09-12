//go:build windows

package platform

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// sizeTiebreak enables the byte-size fallback in SameInstallation. It is needed
// on Windows, where the `claude` shim is a copy rather than a hardlink and so
// has no file identity in common with the clother binary.
const sizeTiebreak = true

// exeExt is the extension the Windows loader requires for an executable.
const exeExt = ".exe"

// ioReparseTagMountPoint is IO_REPARSE_TAG_MOUNT_POINT. Go keeps this constant
// unexported (syscall._IO_REPARSE_TAG_MOUNT_POINT) even though it can already
// *read* junctions, so it is spelled out here. It is a documented, stable part
// of the Windows reparse-point ABI.
const ioReparseTagMountPoint = 0xA0000003

// fsctlSetReparsePoint is FSCTL_SET_REPARSE_POINT.
const fsctlSetReparsePoint = 0x000900A4

// LinkSibling makes link an alias for the file named name in the same
// directory. Windows has no unprivileged symlink, so this is an NTFS hardlink;
// both paths therefore live in the same directory and, by construction, on the
// same volume. If the filesystem cannot do hardlinks (FAT32, exFAT, some
// network shares) the file is copied instead.
func LinkSibling(name, link string) error {
	return LinkOrCopy(siblingPath(name, link), link)
}

// LinkOrCopy makes dst refer to src, preferring a hardlink and falling back to
// a byte-for-byte copy. A hardlink shares the inode, so replacing the original
// later does not disturb an existing link — callers that update the binary must
// recreate their links, which launchers.Sync already does on every run.
func LinkOrCopy(src, dst string) error {
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	return copyFile(src, dst)
}

// LinkDir makes dst a directory junction pointing at src.
//
// A junction is used rather than a symlink because the SYMLINK reparse tag
// requires SeCreateSymbolicLinkPrivilege (Developer Mode or an elevated
// process), whereas the MOUNT_POINT tag does not. Hardlinks cannot stand in
// here: they are file-only. If the native call fails we fall back to mklink /J,
// which is slower (one process per directory) but uses the same mechanism.
func LinkDir(src, dst string) error {
	if err := createJunction(src, dst); err == nil {
		return nil
	}
	return junctionViaMklink(src, dst)
}

func createJunction(target, link string) error {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	absLink, err := filepath.Abs(link)
	if err != nil {
		return err
	}
	if err := os.Mkdir(absLink, 0o755); err != nil {
		return err
	}
	// From here on the directory exists; make sure it does not survive a failure.
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(absLink)
		}
	}()

	// The substitute name is the kernel's own path form; the print name is what
	// Explorer and `dir` show. Both are UTF-16 and NUL-terminated, and every
	// offset below is relative to the start of the path buffer.
	substitute := utf16.Encode([]rune(`\??\` + absTarget))
	print := utf16.Encode([]rune(absTarget))

	const headerLen = 8 // four uint16 fields after the 8-byte tag/length header
	buf := make([]byte, 16+2*(len(substitute)+1)+2*(len(print)+1))

	putUint32 := func(off int, v uint32) {
		*(*uint32)(unsafe.Pointer(&buf[off])) = v
	}
	putUint16 := func(off int, v uint16) {
		*(*uint16)(unsafe.Pointer(&buf[off])) = v
	}
	putUint32(0, ioReparseTagMountPoint)
	// ReparseDataLength sits at offset 4 and covers only the union payload;
	// offset 6 is Reserved and must stay zero.
	putUint16(4, uint16(headerLen+2*(len(substitute)+1)+2*(len(print)+1)))
	putUint16(8, 0)                              // SubstituteNameOffset
	putUint16(10, uint16(2*(len(substitute)+1))) // SubstituteNameLength
	putUint16(12, uint16(2*(len(substitute)+1))) // PrintNameOffset
	putUint16(14, uint16(2*(len(print)+1)))      // PrintNameLength
	pathBase := 16
	for i, unit := range substitute {
		putUint16(pathBase+2*i, unit)
	}
	printBase := pathBase + 2*(len(substitute)+1)
	for i, unit := range print {
		putUint16(printBase+2*i, unit)
	}

	// Opening with FILE_FLAG_OPEN_REPARSE_POINT means we address the directory
	// itself rather than following anything; BACKUP_SEMANTICS is required to
	// open a directory handle at all.
	name, err := syscall.UTF16PtrFromString(absLink)
	if err != nil {
		return err
	}
	handle, err := syscall.CreateFile(
		name,
		syscall.GENERIC_WRITE,
		0,
		nil,
		syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_OPEN_REPARSE_POINT|syscall.FILE_FLAG_BACKUP_SEMANTICS,
		0,
	)
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(handle)

	var returned uint32
	if err := syscall.DeviceIoControl(
		handle,
		fsctlSetReparsePoint,
		&buf[0],
		uint32(len(buf)),
		nil,
		0,
		&returned,
		nil,
	); err != nil {
		return err
	}
	ok = true
	return nil
}

func junctionViaMklink(target, link string) error {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	absLink, err := filepath.Abs(link)
	if err != nil {
		return err
	}
	_ = os.Remove(absLink)
	// mklink is a cmd builtin, not an executable, so it must run through cmd.
	cmd := exec.Command("cmd", "/c", "mklink", "/J", absLink, absTarget)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mklink /J %s %s: %w", absLink, absTarget, err)
	}
	return nil
}

// LinkLauncher makes link a launcher for the clother binary at execPath.
//
// Windows has no unprivileged symlink, so a launcher is an NTFS hardlink: the
// same file record under another name and zero extra bytes. A hardlink does not
// follow the original when the original is replaced, which is why launchers.Sync
// recreates every link after it writes the binary.
func LinkLauncher(execPath, binaryName, link string, absolute bool) error {
	if absolute {
		return LinkOrCopy(execPath, link)
	}
	return LinkSibling(binaryName, link)
}

// LinkShim makes link the `claude` entry that points back at clother.
//
// This one is deliberately a copy, not a hardlink like every other launcher. A
// hardlink would make claude.exe and clother.exe one and the same file record,
// so anything rewriting claude.exe in place would destroy clother itself — and
// claude.exe lands in the bin directory of the user's real Claude Code install,
// which is exactly where Claude Code's own updater looks. Every launcher would
// silently become real Claude Code. A copy confines the damage to the shim,
// which the next `clother install` simply recreates; one 9 MB file is a cheap
// price for that.
func LinkShim(execPath, binaryName, link string, absolute bool) error {
	src := siblingPath(binaryName, link)
	if absolute {
		src = execPath
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return AtomicWrite(link, data, 0o755)
}

func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
