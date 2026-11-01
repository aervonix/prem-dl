// Package selfupdate finds newer Prem-dl releases on the release server and downloads them,
// checked against a manifest signed with Prem-dl's release key. Installing what it downloaded is
// the caller's job (app_update.go): it has to quit the app first.
//
// The server (the TrueRip API) answers only to a key. Prem-dl's repository is public, so it
// can't hold one: release builds carry a build secret instead (secret.go), and each install
// trades it for its own key on first run (Enrol) and keeps that key in its data folder. A copy
// built from source has no secret and doesn't check for updates.
//
// Nothing the server says is trusted on its own: a release is only taken when its manifest
// verifies with the release public key built into the app, names this app and the channel
// asked for, and the downloaded file matches the manifest's size and SHA-256. So even a
// compromised server can't push an update; only the signing key, which never leaves the
// release machine, can make one.
package selfupdate

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Where releases come from. The release public key is public by design: it only checks
// signatures, and the private half stays on the release machine.
const (
	APIURL           = "https://api.truerip.cc"
	App              = "prem-dl"
	ReleasePublicKey = "V+jVd5lCbXASLLOtszV1orMXoKgUh/XbFPamjn67I7E="
)

// Channel is where releases are published. A release has one build per target (TargetWindows,
// TargetAppImage, TargetDeb), and each build asks for its own.
const Channel = "stable"

// The targets: what each kind of release build is, baked into it by the release scripts.
const (
	TargetWindows  = "windows-x64"        // Prem-dl.exe and Prem-dl-Setup.exe; the installer is published
	TargetAppImage = "linux-x64-appimage" // Prem-dl-x86_64.AppImage
	TargetDeb      = "linux-x64-deb"      // prem-dl_<v>_amd64.deb
)

// buildTarget is set by target_release.go, which the release scripts write for each build
// (go run ./cmd/buildsecret -target ...) and delete after it; it is gitignored. Built from
// source, a copy has none.
var buildTarget string

// BuildTarget is the kind of build this is, or "" when it was built from source.
func BuildTarget() string {
	if !targetRe.MatchString(buildTarget) {
		return ""
	}
	return buildTarget
}

// FileFits reports whether a release file is the kind target installs: a manifest for "any
// platform" (no target) names no system, so its file has to say it.
func FileFits(target, fileName string) bool {
	name := strings.ToLower(fileName)
	switch target {
	case TargetWindows:
		return strings.HasSuffix(name, ".exe")
	case TargetAppImage:
		return strings.HasSuffix(name, ".appimage")
	case TargetDeb:
		return strings.HasSuffix(name, ".deb")
	}
	return false
}

var (
	// ErrNoRelease: nothing is published on the channel (or the server doesn't know it).
	ErrNoRelease = errors.New("no Prem-dl release has been published yet")
	// ErrNotAccepted: the server turned the build secret, or this install's key, away.
	ErrNotAccepted = errors.New("the update server doesn't accept this copy of Prem-dl")
	// ErrVerify: the server's answer isn't signed by Prem-dl's release key, or doesn't match it.
	ErrVerify = errors.New("the update didn't verify with Prem-dl's release key, so it was refused")
)

// OfflineError wraps a failure to reach the server: try again later, quietly.
type OfflineError struct{ Err error }

func (e *OfflineError) Error() string { return "couldn't reach the update server: " + e.Err.Error() }
func (e *OfflineError) Unwrap() error { return e.Err }

// Client talks to the release server with one install's key.
type Client struct {
	Base      string // https://api.truerip.cc
	App       string
	Target    string // this build's target; "" asks only for builds for any platform
	Key       string // this install's key (tra_...)
	PublicKey ed25519.PublicKey
	HTTP      *http.Client
}

// PublicKeyOf decodes a base64 release public key.
func PublicKeyOf(b64 string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, errors.New("the release public key isn't a base64 Ed25519 key")
	}
	return ed25519.PublicKey(raw), nil
}

var (
	keyRe    = regexp.MustCompile(`^tra_[0-9A-Za-z]{12}_[0-9A-Za-z]{43}$`)
	secretRe = regexp.MustCompile(`^trb_[0-9A-Za-z]{12}_[0-9A-Za-z_-]{43}$`)
	labelRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+ -]{0,39}$`)
	sha256Re = regexp.MustCompile(`^[0-9a-f]{64}$`)
	targetRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,39}$`)
)

// ValidKey reports whether s looks like an install key.
func ValidKey(s string) bool { return keyRe.MatchString(s) }

// ValidSecret reports whether s looks like a build secret.
func ValidSecret(s string) bool { return secretRe.MatchString(s) }

const userAgent = "Prem-dl"

// Enrol trades the build secret for this install's own key (POST /v1/enrol). platform and
// version describe the copy coarsely ("linux", "0.1.3"), nothing that tells machines apart.
// ErrNotAccepted means the secret was revoked: carry on without updates.
func Enrol(ctx context.Context, hc *http.Client, base, secret, platform, version string) (string, error) {
	body := map[string]string{}
	if labelRe.MatchString(platform) {
		body["platform"] = platform
	}
	if labelRe.MatchString(version) {
		body["app_version"] = version
	}
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(base, "/")+"/v1/enrol", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := hc.Do(req)
	if err != nil {
		return "", &OfflineError{err}
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusCreated:
	case http.StatusNotFound:
		return "", ErrNotAccepted
	case http.StatusTooManyRequests:
		return "", &OfflineError{errors.New("too many new installs at once; it tries again later")}
	default:
		return "", fmt.Errorf("the update server answered %s", resp.Status)
	}
	var out struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&out); err != nil || !ValidKey(out.Key) {
		return "", errors.New("the update server gave no key")
	}
	return out.Key, nil
}

func (c *Client) request(ctx context.Context, path string, header http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(c.Base, "/")+path, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, &OfflineError{err}
	}
	return resp, nil
}

// Ping checks the key once (GET /v1/ping): false means the server dropped it (revoked, or
// unused for 90 days), and the install should enrol again.
func (c *Client) Ping(ctx context.Context) (bool, error) {
	resp, err := c.request(ctx, "/v1/ping", nil)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	}
	return false, fmt.Errorf("the update server answered %s", resp.Status)
}

// Release is a verified release: its fields come from the signed manifest.
type Release struct {
	Version  string
	Channel  string
	Target   string // the build's target; "" for a build for any platform
	FileName string
	Size     int64
	SHA256   string
	Notes    string // Markdown
	Date     string
}

// Latest returns the newest release on channel for this build's target (or for any platform),
// verified. ErrNoRelease when there is none.
func (c *Client) Latest(ctx context.Context, channel string) (Release, error) {
	path := "/v1/releases/" + url.PathEscape(channel) + "/latest"
	if c.Target != "" {
		path += "?" + url.Values{"target": {c.Target}}.Encode()
	}
	resp, err := c.request(ctx, path, nil)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return Release{}, ErrNoRelease
	default:
		return Release{}, fmt.Errorf("the update server answered %s", resp.Status)
	}
	var body struct {
		Manifest  string `json:"manifest"`
		Signature string `json:"signature"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return Release{}, fmt.Errorf("reading the update server's answer: %w", err)
	}
	return Verify(c.PublicKey, c.App, channel, c.Target, body.Manifest, body.Signature)
}

// Verify checks a manifest's signature over its exact bytes, then that it is this app's and this
// channel's, and for target or for any platform (never another target's build), and returns what
// it says. Nothing is read from it before the signature holds.
func Verify(pub ed25519.PublicKey, app, channel, target, manifest, signature string) (Release, error) {
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(pub, []byte(manifest), sig) {
		return Release{}, ErrVerify
	}
	var m struct {
		App      string `json:"app"`
		Channel  string `json:"channel"`
		Date     string `json:"date"`
		FileName string `json:"file_name"`
		Notes    string `json:"notes"`
		SHA256   string `json:"sha256"`
		Size     *int64 `json:"size"`
		Target   string `json:"target"`
		Version  string `json:"version"`
	}
	if err := json.Unmarshal([]byte(manifest), &m); err != nil {
		return Release{}, ErrVerify
	}
	if m.App != app || m.Channel != channel || (m.Target != "" && m.Target != target) ||
		!sha256Re.MatchString(m.SHA256) || m.Size == nil || *m.Size <= 0 || !ValidVersion(m.Version) {
		return Release{}, ErrVerify
	}
	return Release{Version: m.Version, Channel: m.Channel, Target: m.Target, FileName: m.FileName, Size: *m.Size, SHA256: m.SHA256, Notes: m.Notes, Date: m.Date}, nil
}

// ProgressFunc reports bytes downloaded so far and the total.
type ProgressFunc func(done, total int64)

// Download saves r's file as dest, resuming dest+".part" when an earlier try left one, and
// checks its size and SHA-256 against the signed manifest. On a mismatch nothing is kept.
func (c *Client) Download(ctx context.Context, r Release, dest string, progress ProgressFunc) error {
	part := dest + ".part"
	var have int64
	if fi, err := os.Stat(part); err == nil {
		have = fi.Size()
	}
	if have > r.Size {
		_ = os.Remove(part)
		have = 0
	}
	if have < r.Size {
		h := http.Header{}
		if have > 0 {
			h.Set("Range", "bytes="+strconv.FormatInt(have, 10)+"-")
		}
		path := "/v1/releases/" + url.PathEscape(r.Channel) + "/" + url.PathEscape(r.Version) + "/download"
		if r.Target != "" {
			// Exactly the build the manifest describes (there is no fallback here).
			path += "?" + url.Values{"target": {r.Target}}.Encode()
		}
		resp, err := c.request(ctx, path, h)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusOK:
			have = 0 // the server sent it all: start over
		case http.StatusPartialContent:
		case http.StatusNotFound:
			return errors.New("the update was withdrawn while it downloaded; check again later")
		default:
			return fmt.Errorf("downloading the update: the server answered %s", resp.Status)
		}
		flags := os.O_CREATE | os.O_WRONLY
		if have == 0 {
			flags |= os.O_TRUNC
		}
		f, err := os.OpenFile(part, flags, 0o600)
		if err != nil {
			return err
		}
		if _, err := f.Seek(have, io.SeekStart); err != nil {
			f.Close()
			return err
		}
		buf := make([]byte, 256<<10)
		var last time.Time
		var copyErr error
		for {
			n, rerr := resp.Body.Read(buf)
			if n > 0 {
				if _, werr := f.Write(buf[:n]); werr != nil {
					copyErr = werr
					break
				}
				have += int64(n)
				if have > r.Size {
					break
				}
			}
			if progress != nil && (time.Since(last) > 100*time.Millisecond || rerr == io.EOF) {
				last = time.Now()
				progress(have, r.Size)
			}
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				copyErr = &OfflineError{rerr}
				break
			}
		}
		if err := f.Close(); copyErr == nil {
			copyErr = err
		}
		if copyErr != nil {
			return copyErr // the .part stays: the next try resumes it
		}
	}
	sum, size, err := hashFile(part)
	if err != nil {
		return err
	}
	if size != r.Size || sum != r.SHA256 {
		_ = os.Remove(part)
		return errors.New("the downloaded update doesn't match its signed manifest, so it was deleted; try again")
	}
	_ = os.Remove(dest)
	return os.Rename(part, dest)
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
