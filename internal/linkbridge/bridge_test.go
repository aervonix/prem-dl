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
	req := httptest.NewRequest(http.MethodPost, "http://"+host+"/open", strings.NewReader(body))
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
	s := &Server{Origin: origin, Open: func(l string) error { got = append(got, l); return nil }}
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
	s := &Server{Origin: origin, Open: func(string) error { opened++; return nil }}
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
	s := &Server{Origin: origin, Open: func(string) error { return errors.New("not a link") }}
	rec, r := post(t, s, "localhost:41853", origin, `{"url":"ftp://x"}`)
	if rec.Code != http.StatusBadRequest || r.Error != "not a link" || r.App != "prem-dl" {
		t.Fatalf("status %d, reply %+v", rec.Code, r)
	}
	if rec, r := post(t, s, "localhost:41853", origin, `{}`); rec.Code != http.StatusBadRequest || r.Error == "" {
		t.Fatalf("empty body: status %d, reply %+v", rec.Code, r)
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
