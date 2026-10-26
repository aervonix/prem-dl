package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"0.1.1", "v0.2.0", true},
		{"0.1.1", "0.1.2", true},
		{"0.1.1", "0.1.1", false},
		{"0.1.1", "v0.1.1", false},
		{"0.2", "0.2.0", false},
		{"0.2.0", "0.2.1", true},
		{"0.10.0", "0.9.9", false},
		{"0.9.9", "0.10.0", true},
		{"1.0.0", "0.9.0", false},
		{"0.1.1", "nightly", false},
		{"", "0.2.0", false},
		{"0.1.1", "0.2.0-beta", false},
	}
	for _, c := range cases {
		if got := Newer(c.current, c.latest); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

// fakeGitHub serves a release whose installer is setup and whose checksum list says sum.
func fakeGitHub(t *testing.T, setup []byte, sum string) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/" + Repo + "/releases/latest":
			fmt.Fprintf(w, `{"tag_name":"v0.2.0","html_url":"https://github.com/%[1]s/releases/tag/v0.2.0","body":"New things",
				"assets":[{"name":"Prem-dl.exe","browser_download_url":"%[2]s/plain","size":5},
				{"name":"%[3]s","browser_download_url":"%[2]s/setup","size":%[4]d},
				{"name":"%[5]s","browser_download_url":"%[2]s/sums","size":100}]}`,
				Repo, srv.URL, SetupAsset, len(setup), SumsAsset)
		case "/setup":
			w.Write(setup)
		case "/sums":
			fmt.Fprintf(w, "0000  Prem-dl.exe\n%s  %s\n", sum, SetupAsset)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = old })
	return srv
}

func sha(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func TestLatestAndDownload(t *testing.T) {
	setup := []byte(strings.Repeat("installer", 50000))
	srv := fakeGitHub(t, setup, sha(setup))

	r, err := Latest(context.Background(), srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "0.2.0" || r.Notes != "New things" || r.SetupURL != srv.URL+"/setup" || r.SumsURL != srv.URL+"/sums" || r.Size != int64(len(setup)) {
		t.Fatalf("release = %+v", r)
	}
	if !strings.HasSuffix(r.Page, "/releases/tag/v0.2.0") {
		t.Errorf("page = %q", r.Page)
	}

	dir := t.TempDir()
	var lastDone int64
	path, err := Download(context.Background(), srv.Client(), r, dir, func(done, total int64) { lastDone = done })
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "Prem-dl-Setup-0.2.0.exe" {
		t.Errorf("path = %s", path)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(setup) {
		t.Fatalf("installer not saved intact: %v", err)
	}
	if lastDone != int64(len(setup)) {
		t.Errorf("last progress = %d, want %d", lastDone, len(setup))
	}
}

func TestDownloadRejectsBadChecksum(t *testing.T) {
	srv := fakeGitHub(t, []byte("tampered"), sha([]byte("original")))
	r, err := Latest(context.Background(), srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := Download(context.Background(), srv.Client(), r, dir, nil); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err = %v, want a checksum error", err)
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
}

func TestDownloadNeedsChecksums(t *testing.T) {
	r := Release{Version: "0.2.0", SetupURL: "http://127.0.0.1:1/setup"}
	if _, err := Download(context.Background(), http.DefaultClient, r, t.TempDir(), nil); err == nil || !strings.Contains(err.Error(), "checksums") {
		t.Fatalf("err = %v", err)
	}
}
