package cookiebridge

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestExtensionIDMatchesManifestKey(t *testing.T) {
	raw, err := os.ReadFile("../../chromehelper/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	der, err := base64.StdEncoding.DecodeString(m.Key)
	if err != nil {
		t.Fatal(err)
	}
	// Chrome's extension ID: the first 128 bits of SHA-256(key), one letter a-p per nibble.
	sum := sha256.Sum256(der)
	id := make([]byte, 32)
	for i := 0; i < 16; i++ {
		id[2*i] = 'a' + sum[i]>>4
		id[2*i+1] = 'a' + sum[i]&0xf
	}
	if string(id) != ExtensionID {
		t.Fatalf("manifest key gives %s, code expects %s", id, ExtensionID)
	}
}

func TestNetscapeKeepsOnlyYouTube(t *testing.T) {
	data, n := Netscape([]Cookie{
		{Domain: ".youtube.com", Path: "/", Secure: true, HTTPOnly: true, ExpirationDate: 1999999999.7, Name: "SID", Value: "a"},
		{Domain: "www.youtube.com", HostOnly: true, Path: "/", Session: true, Name: "PREF", Value: "b"},
		{Domain: "music.youtube.com", Path: "/", ExpirationDate: 1999999999, Name: "M", Value: "c"},
		{Domain: ".google.com", Path: "/", Name: "NID", Value: "x"},
		{Domain: "notyoutube.com", Path: "/", Name: "X", Value: "x"},
		{Domain: "youtube.com.evil.example", Path: "/", Name: "X", Value: "x"},
		{Domain: ".youtube.com", Path: "/", Name: "TAB", Value: "x\ty"},
	})
	got := string(data)
	if n != 3 {
		t.Fatalf("count = %d\n%s", n, got)
	}
	for _, want := range []string{
		"#HttpOnly_.youtube.com\tTRUE\t/\tTRUE\t1999999999\tSID\ta\n",
		"www.youtube.com\tFALSE\t/\tFALSE\t0\tPREF\tb\n",
		".music.youtube.com\tTRUE\t/\tFALSE\t1999999999\tM\tc\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	for _, bad := range []string{"NID", "notyoutube", "evil", "TAB"} {
		if strings.Contains(got, bad) {
			t.Errorf("%s should have been dropped:\n%s", bad, got)
		}
	}
	if !strings.HasPrefix(got, "# Netscape HTTP Cookie File\n") {
		t.Error("yt-dlp needs the Netscape header on the first line")
	}
}

// helper plays the extension page against a session.
type helper struct {
	t      *testing.T
	base   string
	token  string
	origin string
}

func newHelper(t *testing.T, s *Session) *helper {
	u := s.HelperURL()
	frag := u[strings.Index(u, "#")+1:]
	port, token, _ := strings.Cut(frag, ".")
	return &helper{t: t, base: "http://127.0.0.1:" + port, token: token, origin: "chrome-extension://" + ExtensionID}
}

func (h *helper) do(method, path, body string) int {
	req, _ := http.NewRequest(method, h.base+path, strings.NewReader(body))
	if h.origin != "" {
		req.Header.Set("Origin", h.origin)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

func (h *helper) hello() int { return h.do("GET", "/hello?token="+h.token, "") }
func (h *helper) send(cancelled bool, token string) int {
	return h.do("POST", "/cookies", fmt.Sprintf(`{"token":%q,"cancelled":%v,"cookies":[{"domain":".youtube.com","path":"/","name":"SID","value":"v"}]}`, token, cancelled))
}

func wait(s *Session, open, answer time.Duration) chan error {
	done := make(chan error, 1)
	go func() {
		cookies, err := s.Wait(context.Background(), open, answer)
		if err == nil && (len(cookies) != 1 || cookies[0].Name != "SID") {
			err = fmt.Errorf("unexpected cookies %+v", cookies)
		}
		done <- err
	}()
	time.Sleep(50 * time.Millisecond) // let the server start
	return done
}

func TestSessionDeliversOnce(t *testing.T) {
	s, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	done := wait(s, 2*time.Second, 2*time.Second)
	h := newHelper(t, s)
	if c := h.hello(); c != http.StatusNoContent {
		t.Fatalf("hello: %d", c)
	}
	if c := h.send(false, h.token); c != http.StatusNoContent {
		t.Fatalf("delivery: %d", c)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSessionRejectsStrangers(t *testing.T) {
	s, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	done := wait(s, 2*time.Second, 2*time.Second)
	h := newHelper(t, s)
	if c := h.send(false, strings.Repeat("0", 64)); c != http.StatusForbidden {
		t.Errorf("wrong token: %d", c)
	}
	other := *h
	other.origin = "https://evil.example"
	if c := other.send(false, h.token); c != http.StatusForbidden {
		t.Errorf("wrong origin: %d", c)
	}
	other.origin = ""
	if c := other.send(false, h.token); c != http.StatusForbidden {
		t.Errorf("no origin (e.g. curl or another program): %d", c)
	}
	// The real delivery still goes through, and only once.
	if c := h.send(false, h.token); c != http.StatusNoContent {
		t.Fatalf("delivery: %d", c)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSessionOutcomes(t *testing.T) {
	t.Run("helper never opens", func(t *testing.T) {
		s, _ := Listen()
		if err := <-wait(s, 200*time.Millisecond, time.Second); !errors.Is(err, ErrNotOpened) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("user declines", func(t *testing.T) {
		s, _ := Listen()
		done := wait(s, time.Second, time.Second)
		h := newHelper(t, s)
		h.hello()
		h.send(true, h.token)
		if err := <-done; !errors.Is(err, ErrDeclined) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("opened but no answer", func(t *testing.T) {
		s, _ := Listen()
		done := wait(s, time.Second, 200*time.Millisecond)
		newHelper(t, s).hello()
		if err := <-done; !errors.Is(err, ErrTimedOut) {
			t.Fatalf("err = %v", err)
		}
	})
}
