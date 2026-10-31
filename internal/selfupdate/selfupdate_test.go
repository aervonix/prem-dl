package selfupdate

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestSemverCorpus(t *testing.T) {
	raw, err := os.ReadFile("testdata/semver-corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Ascending [][]string `json:"ascending"`
		Invalid   []string   `json:"invalid"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	for i, group := range c.Ascending {
		for _, v := range group {
			if !ValidVersion(v) {
				t.Errorf("%q should be valid", v)
			}
			for _, w := range group {
				if Compare(v, w) != 0 {
					t.Errorf("%q and %q should rank the same", v, w)
				}
			}
			for _, later := range c.Ascending[i+1:] {
				for _, w := range later {
					if Compare(v, w) >= 0 || Compare(w, v) <= 0 {
						t.Errorf("%q should rank below %q", v, w)
					}
				}
			}
		}
	}
	for _, v := range c.Invalid {
		if ValidVersion(v) {
			t.Errorf("%q should be invalid", v)
		}
	}
	if !Newer("0.1.2", "0.1.3") || Newer("0.1.3", "0.1.3") || Newer("0.2.0", "0.1.9") || !Newer("0.2.0-beta", "0.2.0") || Newer("0.1.2", "v0.2.0") {
		t.Error("Newer")
	}
}

// server is a fake release server: one app, releases signed with a throwaway key.
type server struct {
	t        *testing.T
	priv     ed25519.PrivateKey
	pub      ed25519.PublicKey
	mu       sync.Mutex
	keys     map[string]bool
	latest   map[string]string // channel + "/" + target -> manifest ("" target: any platform)
	sigs     map[string]string
	files    map[string][]byte // version + "/" + target -> file
	queries  []string          // the target asked for on each request
	ranges   []string
	breakAt  int // the download is cut after this many bytes (0: never)
	enrolled int
}

const testSecret = "trb_AAAAAAAAAAAA_BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"

func newServer(t *testing.T) (*server, *httptest.Server) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	s := &server{t: t, priv: priv, pub: pub, keys: map[string]bool{}, latest: map[string]string{}, sigs: map[string]string{}, files: map[string][]byte{}}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return s, srv
}

func (s *server) publish(app, channel, version string, file []byte, tamper func(string) string) {
	s.publishFor(app, channel, "", version, "Prem-dl-Setup.exe", file, tamper)
}

// publishFor publishes a build for target ("" for any platform), as the server serves it: the
// manifest only has "target" when the build has one.
func (s *server) publishFor(app, channel, target, version, fileName string, file []byte, tamper func(string) string) {
	sum := sha256.Sum256(file)
	tf := ""
	if target != "" {
		tf = fmt.Sprintf(`,"target":%q`, target)
	}
	m := fmt.Sprintf(`{"app":%q,"channel":%q,"date":"2026-10-31T12:00:00Z","file_name":%q,"notes":"Fixes","sha256":%q,"size":%d%s,"version":%q}`,
		app, channel, fileName, hex.EncodeToString(sum[:]), len(file), tf, version)
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(s.priv, []byte(m)))
	if tamper != nil {
		m = tamper(m)
	}
	s.mu.Lock()
	s.latest[channel+"/"+target], s.sigs[channel+"/"+target], s.files[version+"/"+target] = m, sig, file
	s.mu.Unlock()
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if r.URL.Path == "/v1/enrol" && r.Method == http.MethodPost {
		if auth != testSecret {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		s.enrolled++
		key := fmt.Sprintf("tra_KKKKKKKKKKK%d_%043d", s.enrolled, s.enrolled)
		s.keys[key] = true
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"key":%q}`, key)
		return
	}
	if !s.keys[auth] {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	target := r.URL.Query().Get("target")
	s.queries = append(s.queries, target)
	switch {
	case r.URL.Path == "/v1/ping":
		_, _ = w.Write([]byte(`{}`))
	case len(parts) == 4 && parts[1] == "releases" && parts[3] == "latest":
		// The target's own build, or else the build for any platform; without a target, only that.
		k := parts[2] + "/" + target
		if _, ok := s.latest[k]; !ok || target == "" {
			k = parts[2] + "/"
		}
		m, ok := s.latest[k]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"manifest": m, "signature": s.sigs[k]})
	case len(parts) == 5 && parts[1] == "releases" && parts[4] == "download":
		file, ok := s.files[parts[3]+"/"+target] // no fallback: exactly the build asked for
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		from := 0
		if rg := r.Header.Get("Range"); rg != "" {
			s.ranges = append(s.ranges, rg)
			from, _ = strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(rg, "bytes="), "-"))
			w.WriteHeader(http.StatusPartialContent)
		}
		body := file[from:]
		if s.breakAt > 0 && len(body) > s.breakAt {
			body = body[:s.breakAt]
			s.breakAt = 0
			_, _ = w.Write(body)
			panic(http.ErrAbortHandler) // the connection drops mid-download
		}
		_, _ = w.Write(body)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func TestEnrolCheckAndDownload(t *testing.T) {
	s, srv := newServer(t)
	ctx := context.Background()
	if _, err := Enrol(ctx, srv.Client(), srv.URL, "trb_wrongwrongwr_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", "linux", "0.1.2"); !errors.Is(err, ErrNotAccepted) {
		t.Fatalf("a wrong secret: %v", err)
	}
	key, err := Enrol(ctx, srv.Client(), srv.URL, testSecret, "linux", "0.1.2")
	if err != nil || !ValidKey(key) {
		t.Fatalf("enrol: %q %v", key, err)
	}
	c := &Client{Base: srv.URL, App: App, Key: key, PublicKey: s.pub, HTTP: srv.Client()}
	if ok, err := c.Ping(ctx); !ok || err != nil {
		t.Fatalf("ping: %v %v", ok, err)
	}
	if _, err := c.Latest(ctx, "stable"); !errors.Is(err, ErrNoRelease) {
		t.Fatalf("nothing published: %v", err)
	}
	file := []byte(strings.Repeat("setup bytes ", 50_000))
	s.publish(App, "stable", "0.1.3", file, nil)
	r, err := c.Latest(ctx, "stable")
	if err != nil || r.Version != "0.1.3" || r.Size != int64(len(file)) || r.Notes != "Fixes" {
		t.Fatalf("latest: %+v %v", r, err)
	}

	// The connection drops partway: the next try resumes from the .part.
	dest := filepath.Join(t.TempDir(), "Prem-dl-Setup.exe")
	s.mu.Lock()
	s.breakAt = 200_000
	s.mu.Unlock()
	var last int64
	if err := c.Download(ctx, r, dest, func(done, _ int64) { last = done }); err == nil {
		t.Fatal("a cut download succeeded")
	}
	if err := c.Download(ctx, r, dest, func(done, _ int64) { last = done }); err != nil {
		t.Fatalf("resume: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != string(file) || last != int64(len(file)) {
		t.Fatalf("downloaded %d bytes, progress %d", len(got), last)
	}
	if len(s.ranges) != 1 || !strings.HasPrefix(s.ranges[0], "bytes=") {
		t.Errorf("resumed with %v", s.ranges)
	}
	if _, err := os.Stat(dest + ".part"); err == nil {
		t.Error("the .part stayed")
	}
}

func TestRefusesWhatDoesntVerify(t *testing.T) {
	s, srv := newServer(t)
	ctx := context.Background()
	key, _ := Enrol(ctx, srv.Client(), srv.URL, testSecret, "", "")
	c := &Client{Base: srv.URL, App: App, Key: key, PublicKey: s.pub, HTTP: srv.Client()}
	for name, publish := range map[string]func(){
		"changed after signing": func() {
			s.publish(App, "stable", "0.1.3", []byte("x"), func(m string) string { return strings.Replace(m, "0.1.3", "9.9.9", 1) })
		},
		"another app": func() { s.publish("other-app", "stable", "0.1.3", []byte("x"), nil) },
		"another channel": func() {
			s.publish(App, "beta", "0.1.3", []byte("x"), nil)
			s.latest["stable/"], s.sigs["stable/"] = s.latest["beta/"], s.sigs["beta/"]
		},
		"signed by another key": func() {
			_, other, _ := ed25519.GenerateKey(rand.Reader)
			s.priv = other
			s.publish(App, "stable", "0.1.3", []byte("x"), nil)
		},
	} {
		publish()
		if _, err := c.Latest(ctx, "stable"); !errors.Is(err, ErrVerify) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestDownloadMustMatchTheManifest(t *testing.T) {
	s, srv := newServer(t)
	ctx := context.Background()
	key, _ := Enrol(ctx, srv.Client(), srv.URL, testSecret, "", "")
	c := &Client{Base: srv.URL, App: App, Key: key, PublicKey: s.pub, HTTP: srv.Client()}
	s.publish(App, "stable", "0.1.3", []byte("the real installer"), nil)
	r, err := c.Latest(ctx, "stable")
	if err != nil {
		t.Fatal(err)
	}
	s.files["0.1.3/"] = []byte("a swapped installer!") // the same length, other bytes
	dest := filepath.Join(t.TempDir(), "setup.exe")
	if err := c.Download(ctx, r, dest, nil); err == nil {
		t.Fatal("a file that doesn't match the manifest was kept")
	}
	for _, p := range []string{dest, dest + ".part"} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s is left behind", p)
		}
	}
}

func TestPingSaysWhenTheKeyIsGone(t *testing.T) {
	s, srv := newServer(t)
	key, _ := Enrol(context.Background(), srv.Client(), srv.URL, testSecret, "", "")
	delete(s.keys, key)
	c := &Client{Base: srv.URL, App: App, Key: key, PublicKey: s.pub, HTTP: srv.Client()}
	if ok, err := c.Ping(context.Background()); ok || err != nil {
		t.Errorf("a dropped key: %v %v", ok, err)
	}
	c.Base = "http://127.0.0.1:1"
	var off *OfflineError
	if _, err := c.Ping(context.Background()); !errors.As(err, &off) {
		t.Errorf("unreachable: %v", err)
	}
}

func TestEmbeddedPublicKeyDecodes(t *testing.T) {
	if _, err := PublicKeyOf(ReleasePublicKey); err != nil {
		t.Fatal(err)
	}
}

// A build asks for its own target: it gets its own build where the version has one, else the
// build for any platform, and never another target's.
func TestTargets(t *testing.T) {
	s, srv := newServer(t)
	ctx := context.Background()
	key, _ := Enrol(ctx, srv.Client(), srv.URL, testSecret, "", "")
	deb := &Client{Base: srv.URL, App: App, Target: TargetDeb, Key: key, PublicKey: s.pub, HTTP: srv.Client()}
	s.publishFor(App, Channel, "", "0.1.4", "Prem-dl-Setup.exe", []byte("bridge"), nil)
	s.publishFor(App, Channel, TargetDeb, "0.1.4", "prem-dl_0.1.4_amd64.deb", []byte("the deb"), nil)
	r, err := deb.Latest(ctx, Channel)
	if err != nil || r.Target != TargetDeb || r.FileName != "prem-dl_0.1.4_amd64.deb" {
		t.Fatalf("own build: %+v %v", r, err)
	}
	dest := filepath.Join(t.TempDir(), "x.deb")
	if err := deb.Download(ctx, r, dest, nil); err != nil {
		t.Fatalf("download: %v", err)
	}
	if got, _ := os.ReadFile(dest); string(got) != "the deb" {
		t.Errorf("downloaded %q", got)
	}
	if q := s.queries[len(s.queries)-1]; q != TargetDeb {
		t.Errorf("the download asked for target %q", q)
	}

	// Another target's build is refused even when the server hands it over.
	appimage := &Client{Base: srv.URL, App: App, Target: TargetAppImage, Key: key, PublicKey: s.pub, HTTP: srv.Client()}
	s.latest[Channel+"/"+TargetAppImage], s.sigs[Channel+"/"+TargetAppImage] = s.latest[Channel+"/"+TargetDeb], s.sigs[Channel+"/"+TargetDeb]
	if _, err := appimage.Latest(ctx, Channel); !errors.Is(err, ErrVerify) {
		t.Errorf("another target's build: %v", err)
	}
	// One without a build of its own gets the any-platform build; whether its file fits is
	// FileFits' call (app_update.go doesn't offer an .exe to an AppImage).
	delete(s.latest, Channel+"/"+TargetAppImage)
	r, err = appimage.Latest(ctx, Channel)
	if err != nil || r.Target != "" || FileFits(TargetAppImage, r.FileName) || !FileFits(TargetWindows, r.FileName) {
		t.Errorf("any-platform fallback: %+v %v", r, err)
	}
}

func TestBuildTarget(t *testing.T) {
	old := buildTarget
	defer func() { buildTarget = old }()
	for in, want := range map[string]string{"": "", TargetDeb: TargetDeb, "Bad Target": ""} {
		buildTarget = in
		if got := BuildTarget(); got != want {
			t.Errorf("BuildTarget(%q) = %q", in, got)
		}
	}
	if !FileFits(TargetAppImage, "Prem-dl-x86_64.AppImage") || FileFits(TargetDeb, "Prem-dl-Setup.exe") || FileFits("", "x.exe") {
		t.Error("FileFits")
	}
}
