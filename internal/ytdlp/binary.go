// Package ytdlp locates, installs, updates and drives the yt-dlp executable.
package ytdlp

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
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"yt-dlp-gui/internal/sys"
)

// Where a resolved binary came from.
const (
	SourceSetting = "setting"
	SourceApp     = "app"
	SourceCwd     = "cwd"
	SourceManaged = "managed"
	SourcePath    = "path"
)

// ErrNotFound means no yt-dlp executable could be located.
var ErrNotFound = errors.New("yt-dlp executable not found")

// Binary is a resolved yt-dlp executable.
type Binary struct {
	Path    string `json:"path"`
	Source  string `json:"source"`
	Version string `json:"version"`
}

// Name is the executable file name on this platform.
func Name() string {
	if runtime.GOOS == "windows" {
		return "yt-dlp.exe"
	}
	return "yt-dlp"
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// Resolve finds yt-dlp. Order: the user's explicit path, next to this app, the working
// directory, the app-managed copy, then PATH. The managed copy is where updates for
// PATH-installed (or read-only) binaries land, so it outranks PATH.
func Resolve(custom, managedDir string) (Binary, error) {
	if custom != "" {
		if isFile(custom) {
			return Binary{Path: custom, Source: SourceSetting}, nil
		}
		return Binary{}, fmt.Errorf("%w: the configured path %q does not exist", ErrNotFound, custom)
	}
	if exe, err := os.Executable(); err == nil {
		if p := filepath.Join(filepath.Dir(exe), Name()); isFile(p) {
			return Binary{Path: p, Source: SourceApp}, nil
		}
	}
	if wd, err := os.Getwd(); err == nil {
		if p := filepath.Join(wd, Name()); isFile(p) {
			return Binary{Path: p, Source: SourceCwd}, nil
		}
	}
	if p := ManagedPath(managedDir); isFile(p) {
		return Binary{Path: p, Source: SourceManaged}, nil
	}
	if p, err := exec.LookPath(Name()); err == nil {
		return Binary{Path: p, Source: SourcePath}, nil
	}
	return Binary{}, ErrNotFound
}

// ManagedPath is where the app installs its own copy of yt-dlp.
func ManagedPath(managedDir string) string {
	return filepath.Join(managedDir, "bin", Name())
}

func run(ctx context.Context, timeout time.Duration, path string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	sys.Quiet(cmd)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}

// Version asks the executable for its version string.
func Version(ctx context.Context, path string) (string, error) {
	out, err := run(ctx, 20*time.Second, path, "--version", "--ignore-config")
	if err != nil {
		return "", fmt.Errorf("running %s --version: %w", filepath.Base(path), err)
	}
	line := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
	if line == "" {
		return "", errors.New("yt-dlp printed no version")
	}
	return line, nil
}

// ---- ffmpeg and JavaScript runtimes ----

// FFmpeg is a located ffmpeg.
type FFmpeg struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

var ffmpegVersionRe = regexp.MustCompile(`ffmpeg version (\S+)`)

// FindFFmpeg looks at the user's path, next to yt-dlp, in the app's own install (managedDir),
// then PATH. Any of them may be empty.
func FindFFmpeg(ctx context.Context, custom, ytdlpPath, managedDir string) FFmpeg {
	name := "ffmpeg"
	if runtime.GOOS == "windows" {
		name = "ffmpeg.exe"
	}
	var candidates []string
	if custom != "" {
		if st, err := os.Stat(custom); err == nil && st.IsDir() {
			candidates = append(candidates, filepath.Join(custom, name))
		} else {
			candidates = append(candidates, custom)
		}
	}
	if ytdlpPath != "" {
		candidates = append(candidates, filepath.Join(filepath.Dir(ytdlpPath), name))
	}
	if managedDir != "" {
		candidates = append(candidates, filepath.Join(FFmpegDir(managedDir), name))
	}
	if p, err := exec.LookPath(name); err == nil {
		candidates = append(candidates, p)
	}
	for _, c := range candidates {
		if !isFile(c) {
			continue
		}
		out, err := run(ctx, 10*time.Second, c, "-version")
		if err != nil {
			continue
		}
		f := FFmpeg{Path: c}
		if m := ffmpegVersionRe.FindStringSubmatch(out); m != nil {
			f.Version = m[1]
		}
		return f
	}
	return FFmpeg{}
}

// JSRuntime is a JavaScript engine yt-dlp can use to solve YouTube's challenges.
type JSRuntime struct {
	Name string `json:"name"` // deno | node | bun | quickjs
	Path string `json:"path"`
}

// DetectJSRuntimes finds the runtimes yt-dlp supports. yt-dlp only enables deno on its own,
// so the others must be passed with --js-runtimes.
func DetectJSRuntimes(ctx context.Context) []JSRuntime {
	candidates := []struct{ name, exe string }{
		{"deno", "deno"}, {"node", "node"}, {"bun", "bun"}, {"quickjs", "qjs"},
	}
	var found []JSRuntime
	for _, c := range candidates {
		p, err := exec.LookPath(c.exe)
		if err != nil {
			continue
		}
		if c.name == "node" && !nodeNewEnough(ctx, p) {
			continue
		}
		found = append(found, JSRuntime{Name: c.name, Path: p})
	}
	return found
}

func nodeNewEnough(ctx context.Context, path string) bool {
	out, err := run(ctx, 10*time.Second, path, "--version")
	if err != nil {
		return false
	}
	major, err := strconv.Atoi(strings.SplitN(strings.TrimPrefix(strings.TrimSpace(out), "v"), ".", 2)[0])
	return err == nil && major >= 20
}

// ---- versions, install and update ----

// UpdateAvailable reports whether installing the channel's latest release would change anything.
// A nightly or master build ends in a build time (2026.10.25.232749) and so compares newer than
// the stable release it followed; after switching back to stable, any difference is an update.
func UpdateAvailable(current, latest, channel string) bool {
	if current == "" {
		return true
	}
	if channel == "stable" && current != latest && len(strings.Split(current, ".")) > 3 {
		return true
	}
	return CompareVersions(current, latest) < 0
}

// CompareVersions compares yt-dlp version strings such as 2026.10.21 or 2026.10.25.232749.
// It returns -1, 0 or 1.
func CompareVersions(a, b string) int {
	as, bs := strings.Split(strings.TrimSpace(a), "."), strings.Split(strings.TrimSpace(b), ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			y, _ = strconv.Atoi(bs[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func channelRepo(channel string) string {
	switch channel {
	case "nightly":
		return "yt-dlp/yt-dlp-nightly-builds"
	case "master":
		return "yt-dlp/yt-dlp-master-builds"
	default:
		return "yt-dlp/yt-dlp"
	}
}

func assetName() string {
	switch runtime.GOOS {
	case "windows":
		switch runtime.GOARCH {
		case "arm64":
			return "yt-dlp_arm64.exe"
		case "386":
			return "yt-dlp_x86.exe"
		}
		return "yt-dlp.exe"
	case "darwin":
		return "yt-dlp_macos"
	default:
		if runtime.GOARCH == "arm64" {
			return "yt-dlp_linux_aarch64"
		}
		return "yt-dlp_linux"
	}
}

// HTTPClient builds a client that honours an optional proxy URL (else the system proxy).
func HTTPClient(proxy string, timeout time.Duration) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if proxy != "" {
		if u, err := url.Parse(proxy); err == nil {
			tr.Proxy = http.ProxyURL(u)
		}
	}
	return &http.Client{Transport: tr, Timeout: timeout}
}

// LatestVersion returns the newest release tag on the channel.
func LatestVersion(ctx context.Context, client *http.Client, channel string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/"+channelRepo(channel)+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Prem-dl")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("checking for updates: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return "", errors.New("GitHub rate-limited the update check; try again in a while")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("update check failed: GitHub answered %s", resp.Status)
	}
	var body struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body); err != nil {
		return "", fmt.Errorf("reading update info: %w", err)
	}
	if body.Tag == "" {
		return "", errors.New("update info had no version")
	}
	return body.Tag, nil
}

// ProgressFunc reports bytes downloaded so far and the total (0 when unknown).
type ProgressFunc func(done, total int64)

func parseChecksum(sums []byte, name string) (string, bool) {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0]), true
		}
	}
	return "", false
}

var tagRe = regexp.MustCompile(`/releases/tag/([A-Za-z0-9._-]+)$`)

// releaseBase is the download URL of the newest release, pinned to its tag. A ".../latest/download/"
// URL is resolved on every request, so a release published between fetching the checksums and the
// binary (nightly and master builds ship often) would make them disagree. The tag is read from the
// redirect GitHub answers "latest" with, which isn't rate-limited like the API; if that fails the
// unpinned URL is used.
func releaseBase(ctx context.Context, client *http.Client, repo string) string {
	latest := "https://github.com/" + repo + "/releases/latest/download/"
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, "https://github.com/"+repo+"/releases/latest", nil)
	if err != nil {
		return latest
	}
	req.Header.Set("User-Agent", "Prem-dl")
	noFollow := *client
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := noFollow.Do(req)
	if err != nil {
		return latest
	}
	resp.Body.Close()
	tag := tagFromLocation(resp.Header.Get("Location"))
	if tag == "" {
		return latest
	}
	return "https://github.com/" + repo + "/releases/download/" + tag + "/"
}

func tagFromLocation(loc string) string {
	if m := tagRe.FindStringSubmatch(loc); m != nil && !strings.Contains(m[1], "..") {
		return m[1]
	}
	return ""
}

// Install downloads the newest yt-dlp for the channel to dest, verifying its SHA-256 against the
// release's checksum file and running it once before swapping it in. A previous file at dest is
// replaced atomically where the OS allows and restored if anything fails.
func Install(ctx context.Context, client *http.Client, channel, dest string, progress ProgressFunc) (string, error) {
	repo := channelRepo(channel)
	name := assetName()
	base := releaseBase(ctx, client, repo)

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}

	sums, err := fetchAll(ctx, client, base+"SHA2-256SUMS", 1<<20)
	if err != nil {
		return "", fmt.Errorf("downloading checksums: %w", err)
	}
	want, ok := parseChecksum(sums, name)
	if !ok {
		return "", fmt.Errorf("no checksum published for %s; refusing to install an unverified binary", name)
	}

	newPath := dest + ".new"
	hash, err := downloadTo(ctx, client, base+name, newPath, progress)
	if err != nil {
		os.Remove(newPath)
		return "", err
	}
	if hash != want {
		os.Remove(newPath)
		return "", errors.New("the downloaded file failed its SHA-256 check and was discarded")
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(newPath, 0o755)
	}
	version, err := Version(ctx, newPath)
	if err != nil {
		os.Remove(newPath)
		return "", fmt.Errorf("the downloaded yt-dlp does not run: %w", err)
	}

	oldPath := dest + ".old"
	hadOld := false
	if isFile(dest) {
		_ = os.Remove(oldPath)
		if err := os.Rename(dest, oldPath); err != nil {
			os.Remove(newPath)
			return "", fmt.Errorf("can't replace %s (is it in use or read-only?): %w", dest, err)
		}
		hadOld = true
	}
	if err := os.Rename(newPath, dest); err != nil {
		if hadOld {
			_ = os.Rename(oldPath, dest)
		}
		os.Remove(newPath)
		return "", fmt.Errorf("installing yt-dlp: %w", err)
	}
	if hadOld {
		_ = os.Remove(oldPath)
	}
	return version, nil
}

func fetchAll(ctx context.Context, client *http.Client, u string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Prem-dl")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", u, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

func downloadTo(ctx context.Context, client *http.Client, u, path string, progress ProgressFunc) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Prem-dl")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", fileOf(u), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading %s: server answered %s", fileOf(u), resp.Status)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	pr := &progressReader{r: resp.Body, total: resp.ContentLength, cb: progress}
	_, copyErr := io.Copy(io.MultiWriter(f, h), pr)
	closeErr := f.Close()
	if copyErr != nil {
		return "", fmt.Errorf("downloading %s: %w", fileOf(u), copyErr)
	}
	if closeErr != nil {
		return "", closeErr
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type progressReader struct {
	r     io.Reader
	done  int64
	total int64
	cb    ProgressFunc
	last  time.Time
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	if p.cb != nil && (time.Since(p.last) > 100*time.Millisecond || err == io.EOF) {
		p.last = time.Now()
		p.cb(p.done, p.total)
	}
	return n, err
}

// fileOf is the last element of a download URL, for error messages.
func fileOf(u string) string {
	return u[strings.LastIndex(u, "/")+1:]
}
