package config

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jolehuit/clother/internal/platform"
)

type Paths struct {
	ConfigDir       string
	DataDir         string
	CacheDir        string
	BinDir          string
	ConfigFile      string
	SecretsFile     string
	ManifestFile    string
	SessionPatchDir string
	UpdateCacheFile string
}

func Detect(binOverride string) (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}

	configDir := getenv("CLOTHER_CONFIG_DIR", platform.DefaultConfigDir(home))
	dataDir := getenv("CLOTHER_DATA_DIR", platform.DefaultDataDir(home))
	cacheDir := getenv("CLOTHER_CACHE_DIR", platform.DefaultCacheDir(home))

	binDir := getenv("CLOTHER_BIN", "")
	if binOverride != "" {
		binDir = binOverride
	}
	if binDir == "" {
		binDir = defaultBinDir(home)
	}

	return Paths{
		ConfigDir:       configDir,
		DataDir:         dataDir,
		CacheDir:        cacheDir,
		BinDir:          binDir,
		ConfigFile:      filepath.Join(configDir, "config.json"),
		SecretsFile:     filepath.Join(dataDir, "secrets.env"),
		ManifestFile:    filepath.Join(dataDir, "launchers.json"),
		SessionPatchDir: filepath.Join(dataDir, "session-patches"),
		UpdateCacheFile: filepath.Join(cacheDir, "update.json"),
	}, nil
}

func (p Paths) EnsureBaseDirs() error {
	for _, dir := range []string{p.ConfigDir, p.DataDir, p.CacheDir, p.SessionPatchDir, p.BinDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func defaultBinDir(home string) string {
	if dir := claudeBinDir(); dir != "" {
		return dir
	}
	return platform.DefaultBinDir(home)
}

func claudeBinDir() string {
	claudePath, err := exec.LookPath("claude")
	if err != nil || claudePath == "" {
		return ""
	}
	if abs, err := filepath.Abs(claudePath); err == nil {
		claudePath = abs
	}
	return filepath.Dir(claudePath)
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
