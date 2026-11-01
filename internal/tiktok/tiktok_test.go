package tiktok

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yt-dlp-gui/internal/ytdlp"
)

// The fixtures in testdata are real pages captured with yt-dlp --write-pages (trimmed to the
// post's item) next to yt-dlp's -J for the same page (cookies and headers left out):
//
//	yt-dlp -J --write-pages --format-sort=res,fps,hdr:12,vbr <post link>
//
// When TikTok changes its pages, capture new ones the same way.
var fixtures = []struct {
	id, handle, kind string
}{
	{"7702605479670828318", "xvbray", "renditions"},
	{"7701966030897286406", "stxtic_daniel", "the player's file only"},
	{"7686135998387883272", "slvtredm", "the player's file beside the renditions"},
	{"7699519368077626646", "haze.effects", "a fast import"},
	{"7701866987609197837", "nasa", "a photo post"},
}

func fixturePost(t *testing.T, id, handle string) *Post {
	t.Helper()
	page, err := os.ReadFile(filepath.Join("testdata", id+".html"))
	if err != nil {
		t.Fatal(err)
	}
	item, err := parsePage(page, id)
	if err != nil {
		t.Fatal(err)
	}
	link := "https://www.tiktok.com/@" + handle + "/video/" + id
	return &Post{item: item, link: link, page: link,
		cookies: []*http.Cookie{{Name: "ttwid", Value: "1|abc"}, {Name: "tt_chain_token", Value: "xyz"}}}
}

func probeOf(t *testing.T, data []byte) *ytdlp.ProbeResult {
	t.Helper()
	res, err := ytdlp.ParseProbe(data)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestParsesLikeYtdlp(t *testing.T) {
	for _, f := range fixtures {
		t.Run(f.kind, func(t *testing.T) {
			data, err := fixturePost(t, f.id, f.handle).Info()
			if err != nil {
				t.Fatal(err)
			}
			theirs, err := os.ReadFile(filepath.Join("testdata", f.id+".ytdlp.json"))
			if err != nil {
				t.Fatal(err)
			}
			compareVideos(t, probeOf(t, data).Video, probeOf(t, theirs).Video)
		})
	}
}

func TestThePlayersFileHasItsSize(t *testing.T) {
	// The post's only file: yt-dlp leaves its size unknown; PlayAddrStruct gives it.
	data, err := fixturePost(t, "7701966030897286406", "stxtic_daniel").Info()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range probeOf(t, data).Video.Formats {
		if f.ID == "play" {
			if f.Filesize != 43170354 {
				t.Errorf("play is %d bytes, want PlayAddrStruct's 43170354", f.Filesize)
			}
			return
		}
	}
	t.Fatal("no play format")
}

func TestFormatsCarryThePagesCookies(t *testing.T) {
	data, err := fixturePost(t, "7702605479670828318", "xvbray").Info()
	if err != nil {
		t.Fatal(err)
	}
	var info struct {
		Headers map[string]string `json:"http_headers"`
		Formats []struct {
			ID      string `json:"format_id"`
			Cookies string `json:"cookies"`
		} `json:"formats"`
	}
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatal(err)
	}
	if got := info.Headers["Referer"]; got != "https://www.tiktok.com/@xvbray/video/7702605479670828318" {
		t.Errorf("Referer %q", got)
	}
	want := "ttwid=1|abc; Domain=.tiktok.com; Path=/; Secure; tt_chain_token=xyz; Domain=.tiktok.com; Path=/; Secure"
	for _, f := range info.Formats {
		if f.Cookies != want {
			t.Errorf("%s: cookies %q", f.ID, f.Cookies)
		}
	}
}

func TestPhotoPost(t *testing.T) {
	post := fixturePost(t, "7701866987609197837", "nasa")
	if !post.Photo() {
		t.Fatal("not seen as a photo post")
	}
	data, err := post.Info()
	if err != nil {
		t.Fatal(err)
	}
	res := probeOf(t, data)
	if res.Kind != "video" || res.Video.Photos != 5 {
		t.Fatalf("kind %s, %d photos", res.Kind, res.Video.Photos)
	}
	if !strings.HasSuffix(res.Video.URL, "/@nasa/photo/7701866987609197837") {
		t.Errorf("URL %s", res.Video.URL)
	}
	if len(res.Video.Formats) != 1 || res.Video.Formats[0].ID != "audio" {
		t.Errorf("formats %+v, want the sound", res.Video.Formats)
	}

	var info struct {
		Type    string `json:"_type"`
		Title   string `json:"title"`
		Entries []struct {
			ID      string `json:"id"`
			Title   string `json:"title"`
			File    string `json:"photo_file"`
			Formats []struct {
				ID            string `json:"format_id"`
				Ext           string `json:"ext"`
				Width, Height int
			} `json:"formats"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatal(err)
	}
	// A multi_video would be joined into one file.
	if info.Type != "playlist" || len(info.Entries) != 6 {
		t.Fatalf("type %q with %d entries", info.Type, len(info.Entries))
	}
	if !strings.HasSuffix(info.Title, "...") {
		t.Errorf("title %q should be cut like yt-dlp's", info.Title)
	}
	for i, e := range info.Entries {
		wantFile, wantID, wantFormat := "Sound", "7701866987609197837", "audio"
		if i > 0 {
			wantFile, wantID, wantFormat = "0"+string(rune('0'+i)), "7701866987609197837-"+string(rune('0'+i)), "picture"
		}
		if e.File != wantFile || e.ID != wantID || len(e.Formats) != 1 || e.Formats[0].ID != wantFormat {
			t.Errorf("entry %d: %+v", i, e)
		}
		if strings.HasSuffix(e.Title, ".") {
			t.Errorf("entry %d's title %q ends in a dot, which Windows drops from its folder", i, e.Title)
		}
		if i > 0 && (e.Formats[0].Ext != "jpg" || e.Formats[0].Width == 0) {
			t.Errorf("picture %d: %+v", i, e.Formats[0])
		}
	}
}

func TestParseLink(t *testing.T) {
	for _, c := range []struct {
		link   string
		ok     bool
		id     string
		photo  bool
		short  bool
		handle string
	}{
		{"https://www.tiktok.com/@nasa/video/7701866987609197837", true, "7701866987609197837", false, false, "nasa"},
		{"https://www.tiktok.com/@nasa/photo/7701866987609197837?lang=en", true, "7701866987609197837", true, false, "nasa"},
		{"https://tiktok.com/@a.b_c/video/123/", true, "123", false, false, "a.b_c"},
		{"https://m.tiktok.com/@/video/123", true, "123", false, false, ""},
		{"https://vm.tiktok.com/ZMabc123/", true, "", false, true, ""},
		{"https://vt.tiktok.com/ZSabc/", true, "", false, true, ""},
		{"https://www.tiktok.com/t/ZTabc/", true, "", false, true, ""},
		{"https://www.tiktok.com/@nasa", false, "", false, false, ""},
		{"https://www.tiktok.com/tag/edit", false, "", false, false, ""},
		{"https://www.tiktok.com/@nasa/live", false, "", false, false, ""},
		{"https://www.tiktok.com.evil.test/@nasa/video/1", false, "", false, false, ""},
		{"ftp://www.tiktok.com/@nasa/video/1", false, "", false, false, ""},
	} {
		l, ok := ParseLink(c.link)
		if ok != c.ok || l.ID != c.id || l.Photo != c.photo || l.Short != c.short || l.Handle != c.handle {
			t.Errorf("%s: %+v %v", c.link, l, ok)
		}
	}
}

// fakeTikTok serves a post page (the xvbray fixture, or what page returns for a path).
func fakeTikTok(t *testing.T, page func(path string) (int, string)) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" || r.Header.Get("Accept-Language") == "" {
			t.Error("asked without a browser's headers")
		}
		if r.URL.Path == "/login" {
			w.Write([]byte("<html>log in</html>"))
			return
		}
		status, body := page(r.URL.Path)
		if status == http.StatusFound {
			http.Redirect(w, r, body, status)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "ttwid", Value: "fresh", Path: "/"})
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func detailPage(detail string) string {
	return `<html><script id="__UNIVERSAL_DATA_FOR_REHYDRATION__" type="application/json">{"__DEFAULT_SCOPE__":{"webapp.video-detail":` +
		detail + `}}</script></html>`
}

func TestReadsAPostPage(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "7702605479670828318.html"))
	if err != nil {
		t.Fatal(err)
	}
	var asked string
	srv := fakeTikTok(t, func(path string) (int, string) {
		asked = path
		return http.StatusOK, string(fixture)
	})
	post, err := read(context.Background(), srv.Client().Transport, srv.URL, "https://www.tiktok.com/@xvbray/video/7702605479670828318")
	if err != nil {
		t.Fatal(err)
	}
	if asked != "/@xvbray/video/7702605479670828318" {
		t.Errorf("asked for %s", asked)
	}
	data, err := post.Info()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"cookies":"ttwid=fresh; Domain=.tiktok.com`) {
		t.Error("the page's cookie isn't on the formats")
	}
	// A photo link asks for the /video/ page, which answers for photo posts too.
	if _, err := read(context.Background(), srv.Client().Transport, srv.URL, "https://www.tiktok.com/@xvbray/photo/7702605479670828318"); err != nil || asked != "/@xvbray/video/7702605479670828318" {
		t.Errorf("photo link: asked %s, %v", asked, err)
	}
}

func TestReadRefusesWhatIsntThePost(t *testing.T) {
	const link = "https://www.tiktok.com/@a/video/123"
	for _, c := range []struct {
		name   string
		status int
		body   string
		check  func(error) bool
	}{
		{"private account", 200, detailPage(`{"statusCode":10222,"statusMsg":"author_secret"}`), func(err error) bool {
			var se *StatusError
			return errors.As(err, &se) && se.Code == 10222
		}},
		{"deleted", 200, detailPage(`{"statusCode":10204,"statusMsg":"item doesn't exist","itemInfo":{}}`), func(err error) bool {
			var se *StatusError
			return errors.As(err, &se) && se.Code == 10204
		}},
		{"withheld", 200, detailPage(`{"statusCode":0,"itemInfo":{"itemStruct":{"id":"123","isContentClassified":true,"desc":"x"}}}`), func(err error) bool {
			return errors.Is(err, ErrWithheld)
		}},
		{"another post", 200, detailPage(`{"statusCode":0,"itemInfo":{"itemStruct":{"id":"999","video":{}}}}`), nil},
		{"no payload", 200, "<html>Please wait...</html>", nil},
		{"broken payload", 200, `<script id="__UNIVERSAL_DATA_FOR_REHYDRATION__">{"__DEFAULT_SCOPE__":</script>`, nil},
		{"refused", 403, "", nil},
		{"sent to log in", 302, "/login", func(err error) bool { return errors.Is(err, ErrWithheld) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := fakeTikTok(t, func(string) (int, string) { return c.status, c.body })
			_, err := read(context.Background(), srv.Client().Transport, srv.URL, link)
			if err == nil || (c.check != nil && !c.check(err)) {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestTruncateLikeYtdlp(t *testing.T) {
	if got := truncate(strings.Repeat("é", 72), 72); got != strings.Repeat("é", 72) {
		t.Errorf("72 characters were cut: %q", got)
	}
	if got := truncate(strings.Repeat("é", 73), 72); got != strings.Repeat("é", 69)+"..." {
		t.Errorf("got %q", got)
	}
}

// compareVideos checks what the UI shows of a post. Only the player file's size may differ:
// yt-dlp leaves it unknown. A photo post's pictures are ours alone (yt-dlp has the sound).
func compareVideos(t *testing.T, ours, theirs *ytdlp.VideoInfo) {
	t.Helper()
	if ours == nil || theirs == nil {
		t.Fatalf("not both videos: %v %v", ours, theirs)
	}
	for _, f := range []struct {
		name string
		a, b any
	}{
		{"id", ours.ID, theirs.ID},
		{"title", ours.Title, theirs.Title},
		{"uploader", ours.Uploader, theirs.Uploader},
		{"duration", ours.Duration, theirs.Duration},
		{"upload date", ours.UploadDate, theirs.UploadDate},
		{"extractor", ours.Extractor, theirs.Extractor},
		{"heights", len(ours.Heights), len(theirs.Heights)},
	} {
		if f.a != f.b {
			t.Errorf("%s: ours %v, yt-dlp's %v", f.name, f.a, f.b)
		}
	}
	theirFormats := map[string]ytdlp.FormatInfo{}
	for _, f := range theirs.Formats {
		theirFormats[f.ID] = f
	}
	if len(ours.Formats) != len(theirs.Formats) {
		t.Errorf("%d formats, yt-dlp has %d", len(ours.Formats), len(theirs.Formats))
	}
	for _, f := range ours.Formats {
		y, ok := theirFormats[f.ID]
		if !ok {
			t.Errorf("format %s isn't yt-dlp's", f.ID)
			continue
		}
		if f.ID == "play" && y.Filesize == 0 {
			y.Filesize = f.Filesize
		}
		if f.Width != y.Width || f.Height != y.Height || f.VCodec != y.VCodec || f.ACodec != y.ACodec ||
			f.Filesize != y.Filesize || f.Ext != y.Ext || f.TBR != y.TBR {
			t.Errorf("format %s:\n ours   %+v\n yt-dlp %+v", f.ID, f, y)
		}
	}
	sized := 0
	for _, f := range ours.Formats {
		if f.Filesize > 0 {
			sized++
		}
	}
	t.Logf("%d formats, %d with a size (yt-dlp's: %d); %d pictures", len(ours.Formats), sized, sizedOf(theirs), ours.Photos)
}

func sizedOf(v *ytdlp.VideoInfo) int {
	n := 0
	for _, f := range v.Formats {
		if f.Filesize > 0 {
			n++
		}
	}
	return n
}
