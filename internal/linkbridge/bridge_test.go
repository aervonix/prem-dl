package linkbridge

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const origin = "chrome-extension://hmphkemhpljgdcibdgcicgmbbhmnnpgp"

func post(t *testing.T, s *Server, host, from, body string) (*httptest.ResponseRecorder, reply) {
	t.Helper()
	return postTo(t, s, "/open", host, from, body)
}

func postTo(t *testing.T, s *Server, path, host, from, body string) (*httptest.ResponseRecorder, reply) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "http://"+host+path, strings.NewReader(body))
	req.Host = host
	if from != "" {
		req.Header.Set("Origin", from)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	var r reply
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	return rec, r
}

func TestOpensLinksFromTheExtension(t *testing.T) {
	var got []string
	s := &Server{Origin: origin, Handle: func(r Request) error { got = append(got, r.URL); return nil }}
	rec, r := post(t, s, "127.0.0.1:38719", origin, `{"url":"https://www.youtube.com/watch?v=dQw4w9WgXcQ"}`)
	if rec.Code != http.StatusOK || r.App != "prem-dl" || r.Error != "" {
		t.Fatalf("status %d, reply %+v", rec.Code, r)
	}
	if len(got) != 1 || got[0] != "https://www.youtube.com/watch?v=dQw4w9WgXcQ" {
		t.Fatalf("opened %q", got)
	}
}

func TestRefusesAnythingElse(t *testing.T) {
	opened := 0
	s := &Server{Origin: origin, Handle: func(Request) error { opened++; return nil }}
	body := `{"url":"https://example.com/v"}`
	for name, c := range map[string]struct{ host, origin string }{
		"a web page":          {"127.0.0.1:38719", "https://evil.example"},
		"no origin":           {"127.0.0.1:38719", ""},
		"another extension":   {"127.0.0.1:38719", "chrome-extension://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		"a rebound host name": {"evil.example:38719", origin},
		"a host without port": {"127.0.0.1", origin},
	} {
		if rec, _ := post(t, s, c.host, c.origin, body); rec.Code != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", name, rec.Code)
		}
	}
	if opened != 0 {
		t.Fatalf("opened %d links", opened)
	}
}

func TestReportsBadLinks(t *testing.T) {
	s := &Server{Origin: origin, Handle: func(Request) error { return errors.New("not a link") }}
	rec, r := post(t, s, "localhost:41853", origin, `{"url":"ftp://x"}`)
	if rec.Code != http.StatusBadRequest || r.Error != "not a link" || r.App != "prem-dl" {
		t.Fatalf("status %d, reply %+v", rec.Code, r)
	}
	if rec, r := post(t, s, "localhost:41853", origin, `{}`); rec.Code != http.StatusBadRequest || r.Error == "" {
		t.Fatalf("empty body: status %d, reply %+v", rec.Code, r)
	}
}

// The menu's quick actions: a download with a preset, or a follow. Every reply names the presets.
func TestActionsAndPresets(t *testing.T) {
	var got []Request
	s := &Server{Origin: origin, Handle: func(r Request) error { got = append(got, r); return nil }, Presets: func() []string { return []string{"Music", "Archive"} }}
	rec, r := post(t, s, "127.0.0.1:38719", origin, `{"url":"https://youtu.be/x","action":"download","preset":"Music"}`)
	if rec.Code != http.StatusOK || len(got) != 1 || got[0].Action != ActionDownload || got[0].Preset != "Music" {
		t.Fatalf("status %d, got %+v", rec.Code, got)
	}
	if strings.Join(r.Presets, ",") != "Music,Archive" {
		t.Errorf("presets %v", r.Presets)
	}
	if rec, _ := post(t, s, "127.0.0.1:38719", origin, `{"url":"https://youtu.be/x","action":"delete"}`); rec.Code != http.StatusBadRequest || len(got) != 1 {
		t.Errorf("an unknown action: status %d", rec.Code)
	}
	if rec, r := postTo(t, s, "/hello", "127.0.0.1:38719", origin, ``); rec.Code != http.StatusOK || len(r.Presets) != 2 {
		t.Errorf("hello: status %d, %+v", rec.Code, r)
	}
	if rec, _ := postTo(t, s, "/hello", "127.0.0.1:38719", "https://evil.example", ``); rec.Code != http.StatusForbidden {
		t.Errorf("hello from a web page: status %d", rec.Code)
	}
}

func TestListenTakesTheNextFreePort(t *testing.T) {
	first, err := Listen()
	if err != nil {
		t.Skip("no port free:", err)
	}
	defer first.Close()
	second, err := Listen()
	if err != nil {
		t.Skip("only one port free:", err)
	}
	defer second.Close()
	if first.Addr().String() == second.Addr().String() {
		t.Fatal("both listeners on the same port")
	}
}
