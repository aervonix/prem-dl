package sys

import (
	"path/filepath"
	"testing"
)

func TestFreeSpace(t *testing.T) {
	dir := t.TempDir()
	free, err := FreeSpace(dir)
	if err != nil || free == 0 {
		t.Fatalf("FreeSpace(%s) = %d, %v", dir, free, err)
	}
	// A folder that doesn't exist yet is measured where it will be made.
	if got, err := FreeSpace(filepath.Join(dir, "not", "yet")); err != nil || got == 0 {
		t.Errorf("missing folder: %d, %v", got, err)
	}
}
