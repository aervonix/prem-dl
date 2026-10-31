package sys

import (
	"os"
	"path/filepath"
)

// FreeSpace returns the bytes free to this user on the drive holding dir. A folder that doesn't
// exist yet (a download folder made on first use) is measured at its nearest existing parent.
func FreeSpace(dir string) (uint64, error) {
	dir = filepath.Clean(dir)
	for {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return freeSpace(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return freeSpace(dir)
		}
		dir = parent
	}
}
