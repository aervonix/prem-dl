//go:build !windows

package ytdlp

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// userRuntimePaths is where per-user installers put a runtime. A terminal's PATH has them, but an
// app started from the desktop, the dock or a link gets the session's PATH, which doesn't: nvm,
// fnm, Volta, asdf and mise for Node (newest version first), ~/.deno and ~/.bun.
func userRuntimePaths(exe string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var paths []string
	switch exe {
	case "node":
		nvm := os.Getenv("NVM_DIR")
		if nvm == "" {
			nvm = filepath.Join(home, ".nvm")
		}
		paths = append(paths, newestFirst(filepath.Join(nvm, "versions", "node", "*", "bin", "node"))...)
		fnm := os.Getenv("FNM_DIR")
		if fnm == "" {
			fnm = filepath.Join(home, ".local", "share", "fnm")
		}
		paths = append(paths, newestFirst(filepath.Join(fnm, "node-versions", "*", "installation", "bin", "node"))...)
		paths = append(paths,
			filepath.Join(home, ".volta", "bin", "node"),
			filepath.Join(home, ".asdf", "shims", "node"),
			filepath.Join(home, ".local", "share", "mise", "shims", "node"))
	case "deno":
		paths = append(paths, filepath.Join(home, ".deno", "bin", "deno"))
	case "bun":
		paths = append(paths, filepath.Join(home, ".bun", "bin", "bun"))
	}
	return slices.DeleteFunc(paths, func(p string) bool {
		st, err := os.Stat(p)
		return err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0o111 == 0
	})
}

// newestFirst globs per-version installs (…/v22.11.0/…) and orders them newest version first.
func newestFirst(pattern string) []string {
	paths, _ := filepath.Glob(pattern)
	version := func(p string) []int {
		for _, part := range strings.Split(filepath.ToSlash(p), "/") {
			if v, ok := strings.CutPrefix(part, "v"); ok {
				var nums []int
				for _, n := range strings.Split(v, ".") {
					i, err := strconv.Atoi(n)
					if err != nil {
						nums = nil
						break
					}
					nums = append(nums, i)
				}
				if len(nums) > 0 {
					return nums
				}
			}
		}
		return nil
	}
	slices.SortStableFunc(paths, func(a, b string) int { return slices.Compare(version(b), version(a)) })
	return paths
}
