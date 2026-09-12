// Command zipasset writes a zip archive containing the named files, each
// stored under its own base name.
//
//	zipasset -o clother_windows_amd64.zip clother.exe
//
// It exists because `zip` is not available everywhere the release gets
// packaged. Git Bash on Windows does not ship it, which is where the Windows
// CI job packages a release, and neither does a minimal Linux image. The Go
// toolchain is required to build the binaries in the first place, so using it
// here removes the last external tool from packaging and makes the archive
// come out the same on every platform.
package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func main() {
	out := flag.String("o", "", "archive to write")
	flag.Parse()
	if *out == "" || flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: zipasset -o archive.zip file...")
		os.Exit(2)
	}
	if err := run(*out, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, "zipasset:", err)
		os.Exit(1)
	}
}

func run(out string, files []string) (err error) {
	dst, err := os.Create(out)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := dst.Close(); err == nil {
			err = cerr
		}
	}()

	w := zip.NewWriter(dst)
	for _, name := range files {
		if err := add(w, name); err != nil {
			return err
		}
	}
	return w.Close()
}

func add(w *zip.Writer, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}

	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	// The archive holds clother.exe, not windows-amd64/clother.exe, so that
	// Expand-Archive drops the binary straight into the directory it is given.
	header.Name = filepath.Base(path)
	header.Method = zip.Deflate

	entry, err := w.CreateHeader(header)
	if err != nil {
		return err
	}
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()

	_, err = io.Copy(entry, src)
	return err
}
