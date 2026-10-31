package ytweb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPeekVideo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("url") != "https://youtu.be/x" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"title":"Never Gonna","author_name":"Rick Astley","thumbnail_url":"https://i.ytimg.com/vi/x/hqdefault.jpg"}`))
	}))
	defer srv.Close()
	old := oembed
	oembed = srv.URL
	defer func() { oembed = old }()
	p, err := PeekVideo(context.Background(), Options{}, "https://youtu.be/x")
	if err != nil || p.Title != "Never Gonna" || p.Uploader != "Rick Astley" || p.Thumbnail == "" {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := PeekVideo(context.Background(), Options{}, "https://youtu.be/missing"); err == nil {
		t.Error("a missing video peeked")
	}
}
