package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"github.com/jolehuit/clother/internal/platform"
)

func DownloadLatestIfNewer(ctx context.Context, current string) (string, string, func(), error) {
	if os.Getenv("CLOTHER_SKIP_SELF_UPDATE") == "1" {
		return "", "", nil, nil
	}

	meta, err := fetchMetadata(ctx, metadataURL())
	if err != nil {
		return "", "", nil, err
	}
	if !isNewer(meta.Version, current) {
		return "", "", nil, nil
	}

	version := displayVersion(meta.Version)
	binaryPath, cleanup, err := downloadReleaseBinary(ctx, version)
	if err != nil {
		return "", "", nil, err
	}
	return binaryPath, version, cleanup, nil
}

func downloadReleaseBinary(ctx context.Context, version string) (string, func(), error) {
	tmpDir, err := os.MkdirTemp("", "clother-update-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	assetName, err := releaseAssetName()
	if err != nil {
		cleanup()
		return "", nil, err
	}

	assetPath := filepath.Join(tmpDir, assetName)
	checksumsPath := filepath.Join(tmpDir, "checksums.txt")
	binaryPath := filepath.Join(tmpDir, platform.BinaryName())

	if err := downloadFile(ctx, releaseAssetURL(version, assetName), assetPath); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := downloadFile(ctx, releaseAssetURL(version, "checksums.txt"), checksumsPath); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := verifyChecksum(assetPath, checksumsPath, assetName); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := extractBinary(assetPath, binaryPath); err != nil {
		cleanup()
		return "", nil, err
	}
	return binaryPath, cleanup, nil
}

// releaseAssetName is the asset this platform downloads from a release. The
// archive format follows the platform, not a preference: a Unix release ships
// .tar.gz so the executable bit survives, while a Windows release ships .zip,
// which is what PowerShell can expand without extra tooling.
func releaseAssetName() (string, error) {
	switch goruntime.GOOS {
	case "darwin", "linux", "windows":
	default:
		return "", fmt.Errorf("unsupported operating system %q", goruntime.GOOS)
	}

	var arch string
	switch goruntime.GOARCH {
	case "amd64":
		arch = "amd64"
	case "arm64":
		arch = "arm64"
	default:
		return "", fmt.Errorf("unsupported architecture %q", goruntime.GOARCH)
	}

	return fmt.Sprintf("clother_%s_%s%s", goruntime.GOOS, arch, releaseArchiveExt()), nil
}

// releaseArchiveExt returns the archive extension used by release assets on
// this platform, including the leading dot.
func releaseArchiveExt() string {
	if platform.IsWindows {
		return ".zip"
	}
	return ".tar.gz"
}

func releaseAssetURL(version, asset string) string {
	if base := strings.TrimRight(strings.TrimSpace(os.Getenv("CLOTHER_RELEASE_BASE_URL")), "/"); base != "" {
		return base + "/" + asset
	}
	return releasesBaseURL() + "/download/" + displayVersion(version) + "/" + asset
}

func downloadFile(ctx context.Context, url, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "clother-install")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: status %s", url, resp.Status)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".download-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func verifyChecksum(assetPath, checksumsPath, assetName string) error {
	data, err := os.ReadFile(checksumsPath)
	if err != nil {
		return err
	}

	expected := ""
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if filepath.Base(fields[len(fields)-1]) == assetName {
			expected = fields[0]
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("checksum for %s not found", assetName)
	}

	file, err := os.Open(assetPath)
	if err != nil {
		return err
	}
	defer file.Close()

	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return err
	}
	if got := hex.EncodeToString(sum.Sum(nil)); !strings.EqualFold(got, expected) {
		return fmt.Errorf("checksum mismatch for %s", assetName)
	}
	return nil
}

// binaryEntryNames are the archive members accepted as the clother executable.
// The Windows member keeps its .exe, the Unix one does not; both are listed on
// both platforms so that an archive cross-built with the other platform's
// layout still unpacks instead of failing with a confusing "not found".
var binaryEntryNames = map[string]bool{
	"clother":     true,
	"clother.exe": true,
}

func extractBinary(assetPath, binaryPath string) error {
	if strings.HasSuffix(strings.ToLower(assetPath), ".zip") {
		return extractBinaryZip(assetPath, binaryPath)
	}
	return extractBinaryTarGz(assetPath, binaryPath)
}

// writeBinary streams the archive member through a temporary file in the
// destination directory and renames it into place, so a failed download never
// leaves a half-written executable behind.
func writeBinary(r io.Reader, binaryPath string) error {
	tmp, err := os.CreateTemp(filepath.Dir(binaryPath), ".binary-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	// A no-op on Windows, where the mode only carries the read-only flag;
	// meaningful on Unix, where the extracted binary must be executable.
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, binaryPath)
}

func extractBinaryTarGz(assetPath, binaryPath string) error {
	file, err := os.Open(assetPath)
	if err != nil {
		return err
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if !binaryEntryNames[filepath.Base(filepath.FromSlash(header.Name))] {
			continue
		}
		return writeBinary(tr, binaryPath)
	}
	return fmt.Errorf("clother binary not found in %s", assetPath)
}

func extractBinaryZip(assetPath, binaryPath string) error {
	archive, err := zip.OpenReader(assetPath)
	if err != nil {
		return err
	}
	defer archive.Close()

	for _, entry := range archive.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		if !binaryEntryNames[filepath.Base(filepath.FromSlash(entry.Name))] {
			continue
		}
		rc, err := entry.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		return writeBinary(rc, binaryPath)
	}
	return fmt.Errorf("clother binary not found in %s", assetPath)
}
