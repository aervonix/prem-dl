// Package selfupdate finds newer Prem-dl releases on GitHub and downloads the installer from one.
// Running the installer is the caller's job (app_update.go): it has to quit the app first.
package selfupdate

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Repo is where releases are published (scripts/release.ps1 builds their files).
const Repo = "aervonix/prem-dl"

// The files every release carries.
const (
	SetupAsset = "Prem-dl-Setup.exe"
	SumsAsset  = "SHA256SUMS.txt"
)

// The Linux files a release may carry (scripts/release.sh); the .deb's name has the version in it.
const (
	AppImageAsset  = "Prem-dl-x86_64.AppImage"
	LinuxSumsAsset = "SHA256SUMS-linux.txt"
)

func isDeb(name string) bool {
	return strings.HasPrefix(name, "prem-dl_") && strings.HasSuffix(name, "_amd64.deb")
}

// apiBase is GitHub's API; tests point it at a local server.
var apiBase = "https://api.github.com"

// UseAPI points the checks at another server that answers like GitHub's API, for trying the
// update flow without publishing a release (dev builds only; see app_update.go).
func UseAPI(base string) { apiBase = strings.TrimSuffix(base, "/") }

// Release is the newest published release.
type Release struct {
	Version  string // without the leading "v"
	Page     string // the release on github.com
	Notes    string // its description, as Markdown
	SetupURL string // "" if the release has no installer
	Size     int64  // the installer's size
	SumsURL  string
	Linux    bool // it has a Linux build (the AppImage or the .deb)
}

// HasBuildFor reports whether the release has something for goos to install. Linux builds came
// later and may be missing from a release; every release has the Windows ones.
func (r Release) HasBuildFor(goos string) bool {
	if goos == "linux" {
		return r.Linux
	}
	return true
}

// Latest asks GitHub for the newest release. Drafts and pre-releases are never "latest".
func Latest(ctx context.Context, client *http.Client) (Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/repos/"+Repo+"/releases/latest", nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Prem-dl")
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("checking for updates: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return Release{}, errors.New("GitHub rate-limited the update check; try again in a while")
	}
	if resp.StatusCode == http.StatusNotFound {
		// Also what a private repository looks like from outside.
		return Release{}, errors.New("no published Prem-dl release was found")
	}
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("update check failed: GitHub answered %s", resp.Status)
	}
	var body struct {
		Tag    string `json:"tag_name"`
		Page   string `json:"html_url"`
		Notes  string `json:"body"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body); err != nil {
		return Release{}, fmt.Errorf("reading update info: %w", err)
	}
	if body.Tag == "" {
		return Release{}, errors.New("update info had no version")
	}
	r := Release{Version: strings.TrimPrefix(body.Tag, "v"), Page: body.Page, Notes: body.Notes}
	if r.Page == "" {
		r.Page = "https://github.com/" + Repo + "/releases/tag/" + body.Tag
	}
	for _, a := range body.Assets {
		switch a.Name {
		case SetupAsset:
			r.SetupURL, r.Size = a.URL, a.Size
		case SumsAsset:
			r.SumsURL = a.URL
		case AppImageAsset:
			r.Linux = true
		default:
			r.Linux = r.Linux || isDeb(a.Name)
		}
	}
	return r, nil
}

// Newer reports whether latest is a later version than current. Versions are dotted numbers
// ("0.1.1", "v0.2"); a missing part counts as 0 and anything unreadable is never newer.
func Newer(current, latest string) bool {
	c, ok1 := parse(current)
	l, ok2 := parse(latest)
	if !ok1 || !ok2 {
		return false
	}
	for i := 0; i < len(c) || i < len(l); i++ {
		var a, b int
		if i < len(c) {
			a = c[i]
		}
		if i < len(l) {
			b = l[i]
		}
		if a != b {
			return b > a
		}
	}
	return false
}

func parse(v string) ([]int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" {
		return nil, false
	}
	var out []int
	for _, p := range strings.Split(v, ".") {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// ProgressFunc reports bytes downloaded so far and the total (0 when unknown).
type ProgressFunc func(done, total int64)

// Download saves the release's installer in dir and checks it against the release's SHA-256
// list. It returns the installer's path; nothing is left behind when it fails.
func Download(ctx context.Context, client *http.Client, r Release, dir string, progress ProgressFunc) (string, error) {
	if r.SetupURL == "" {
		return "", errors.New("this release has no installer")
	}
	if r.SumsURL == "" {
		return "", errors.New("this release has no checksums, so its installer can't be checked")
	}
	sums, err := fetch(ctx, client, r.SumsURL, 64<<10)
	if err != nil {
		return "", err
	}
	want, ok := checksum(sums, SetupAsset)
	if !ok {
		return "", fmt.Errorf("%s doesn't list %s", SumsAsset, SetupAsset)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, "Prem-dl-Setup-"+r.Version+".exe")
	part := dest + ".part"
	got, err := downloadTo(ctx, client, r.SetupURL, part, progress)
	if err != nil {
		os.Remove(part)
		return "", err
	}
	if got != want {
		os.Remove(part)
		return "", errors.New("the downloaded installer doesn't match the release's checksum; try again")
	}
	_ = os.Remove(dest)
	if err := os.Rename(part, dest); err != nil {
		os.Remove(part)
		return "", err
	}
	return dest, nil
}

func checksum(sums []byte, name string) (string, bool) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0]), true
		}
	}
	return "", false
}

func get(ctx context.Context, client *http.Client, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Prem-dl")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", fileOf(u), err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("downloading %s: server answered %s", fileOf(u), resp.Status)
	}
	return resp, nil
}

func fetch(ctx context.Context, client *http.Client, u string, limit int64) ([]byte, error) {
	resp, err := get(ctx, client, u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

func downloadTo(ctx context.Context, client *http.Client, u, path string, progress ProgressFunc) (string, error) {
	resp, err := get(ctx, client, u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	var done int64
	var last time.Time
	buf := make([]byte, 64<<10)
	var copyErr error
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				copyErr = werr
				break
			}
			h.Write(buf[:n])
			done += int64(n)
		}
		if progress != nil && (time.Since(last) > 100*time.Millisecond || rerr == io.EOF) {
			last = time.Now()
			progress(done, resp.ContentLength)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			copyErr = rerr
			break
		}
	}
	closeErr := f.Close()
	if copyErr != nil {
		return "", fmt.Errorf("downloading %s: %w", fileOf(u), copyErr)
	}
	if closeErr != nil {
		return "", closeErr
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// fileOf is the last element of a download URL, for error messages.
func fileOf(u string) string {
	return u[strings.LastIndex(u, "/")+1:]
}
