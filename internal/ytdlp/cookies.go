package ytdlp

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"yt-dlp-gui/internal/settings"
)

const maxCookieFile = 20 << 20

// CookieInfo summarises a cookies.txt file. It never contains cookie values.
type CookieInfo struct {
	Path        string   `json:"path"`
	Valid       bool     `json:"valid"`
	Problem     string   `json:"problem"`
	Count       int      `json:"count"`
	Expired     int      `json:"expired"`
	Session     int      `json:"session"`
	Domains     []string `json:"domains"` // up to 12, most cookies first
	DomainCount int      `json:"domainCount"`
}

// inspect parses Netscape cookie data. now is injected so expiry checks are testable.
func inspect(data []byte, now time.Time) CookieInfo {
	info := CookieInfo{Domains: []string{}}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")

	first := ""
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			first = strings.TrimSpace(l)
			break
		}
	}
	switch {
	case first == "":
		info.Problem = "The file is empty."
		return info
	case strings.HasPrefix(first, "[") || strings.HasPrefix(first, "{"):
		info.Problem = "This looks like a JSON export. yt-dlp needs the Netscape “cookies.txt” format — most cookie-export extensions have a cookies.txt option."
		return info
	case !strings.HasPrefix(first, "# Netscape HTTP Cookie File") && !strings.HasPrefix(first, "# HTTP Cookie File"):
		info.Problem = "The first line must be “# Netscape HTTP Cookie File”. Re-export the cookies in cookies.txt (Netscape) format."
		return info
	}

	perDomain := map[string]int{}
	for _, l := range lines {
		l = strings.TrimSpace(l)
		l = strings.TrimPrefix(l, "#HttpOnly_")
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		f := strings.Split(l, "\t")
		if len(f) != 7 {
			continue
		}
		info.Count++
		perDomain[strings.TrimPrefix(f[0], ".")]++
		exp, err := strconv.ParseInt(f[4], 10, 64)
		switch {
		case err != nil || exp == 0:
			info.Session++
		case time.Unix(exp, 0).Before(now):
			info.Expired++
		}
	}
	if info.Count == 0 {
		info.Problem = "No cookies were found in the file. Make sure the fields are separated by tabs."
		return info
	}

	domains := make([]string, 0, len(perDomain))
	for d := range perDomain {
		domains = append(domains, d)
	}
	sort.Slice(domains, func(i, j int) bool {
		if perDomain[domains[i]] != perDomain[domains[j]] {
			return perDomain[domains[i]] > perDomain[domains[j]]
		}
		return domains[i] < domains[j]
	})
	info.DomainCount = len(domains)
	if len(domains) > 12 {
		domains = domains[:12]
	}
	info.Domains = domains
	info.Valid = true
	return info
}

// InspectCookieFile reads and summarises a cookies.txt file.
func InspectCookieFile(path string) CookieInfo {
	info := CookieInfo{Path: path, Domains: []string{}}
	f, err := os.Open(path)
	if err != nil {
		info.Problem = friendlyFileError(err)
		return info
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxCookieFile+1))
	if err != nil {
		info.Problem = friendlyFileError(err)
		return info
	}
	if len(data) > maxCookieFile {
		info.Problem = "The file is larger than 20 MB, which is too big to be a cookies file."
		return info
	}
	res := inspect(data, time.Now())
	res.Path = path
	return res
}

func friendlyFileError(err error) string {
	if errors.Is(err, os.ErrNotExist) {
		return "The file doesn't exist (it may have been moved or deleted)."
	}
	if errors.Is(err, os.ErrPermission) {
		return "The file can't be read (permission denied)."
	}
	return err.Error()
}

// ImportCookies validates cookie text and stores a normalised private copy as dir/cookies.txt.
func ImportCookies(data []byte, dir string) (CookieInfo, error) {
	if len(data) > maxCookieFile {
		return CookieInfo{}, errors.New("that is too large to be a cookies file")
	}
	info := inspect(data, time.Now())
	if !info.Valid {
		return info, errors.New(info.Problem)
	}
	clean := bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	clean = bytes.ReplaceAll(clean, []byte("\r\n"), []byte("\n"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return info, err
	}
	dst := filepath.Join(dir, "cookies.txt")
	if err := settings.WriteFileAtomic(dst, clean, 0o600); err != nil {
		return info, fmt.Errorf("saving cookies: %w", err)
	}
	info.Path = dst
	return info, nil
}

// PrivateCookieCopy copies src to a temp file for one yt-dlp run. yt-dlp rewrites its cookie
// jar on exit, so concurrent downloads must not share a file. Call the returned func when done.
func PrivateCookieCopy(src string) (string, func(), error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return "", func() {}, fmt.Errorf("cookies file: %s", friendlyFileError(err))
	}
	tmp, err := os.CreateTemp("", "ytgui-cookies-*.txt")
	if err != nil {
		return "", func() {}, err
	}
	name := tmp.Name()
	cleanup := func() { os.Remove(name) }
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return name, cleanup, nil
}
