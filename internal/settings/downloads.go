package settings

import (
	"os"
	"path/filepath"
)

// DownloadsFolder is the user's Downloads folder, the first-run download folder: the one the
// system names (downloadsFolder: Windows' known folder, which can be moved to another drive or
// OneDrive; the XDG user directory on Linux, which can have another name or language), else
// "Downloads" in the home folder.
func DownloadsFolder() string {
	if dir := downloadsFolder(); dir != "" && filepath.IsAbs(dir) {
		return filepath.Clean(dir)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, "Downloads")
}
