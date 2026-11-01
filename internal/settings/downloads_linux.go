package settings

import (
	"os"
	"path/filepath"
)

// downloadsFolder is XDG_DOWNLOAD_DIR from user-dirs.dirs, as xdg-user-dir reads it.
func downloadsFolder() string {
	home, _ := os.UserHomeDir()
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" && home != "" {
		config = filepath.Join(home, ".config")
	}
	data, err := os.ReadFile(filepath.Join(config, "user-dirs.dirs"))
	if err != nil {
		return ""
	}
	return xdgDownloadDir(string(data), home)
}
