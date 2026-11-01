package settings

import (
	"path/filepath"
	"strings"
)

// xdgDownloadDir reads XDG_DOWNLOAD_DIR from a user-dirs.dirs file: a shell assignment,
// `XDG_DOWNLOAD_DIR="$HOME/Downloads"`, quoted, and either absolute or under $HOME. The home
// folder itself means "not set" (xdg-user-dirs writes that for a removed folder), as does
// anything else.
func xdgDownloadDir(file, home string) string {
	for _, line := range strings.Split(file, "\n") {
		line = strings.TrimSpace(line)
		v, ok := strings.CutPrefix(line, "XDG_DOWNLOAD_DIR=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) < 2 || v[0] != '"' || v[len(v)-1] != '"' {
			return ""
		}
		v = strings.ReplaceAll(v[1:len(v)-1], `\"`, `"`)
		switch {
		case strings.HasPrefix(v, "$HOME/"):
			if home == "" {
				return ""
			}
			v = filepath.Join(home, v[len("$HOME/"):])
		case strings.HasPrefix(v, "/"):
		default:
			return ""
		}
		if home != "" && filepath.Clean(v) == filepath.Clean(home) {
			return ""
		}
		return v
	}
	return ""
}
