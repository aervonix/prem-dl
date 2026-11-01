package tiktok

import (
	"net/url"
	"regexp"
	"strings"
)

// postPath is a post's path on www.tiktok.com: /@handle/video/ID or /@handle/photo/ID. The
// handle can be empty (yt-dlp's /@/video/ID) and is decorative: TikTok answers any handle
// with the post's own author.
var postPath = regexp.MustCompile(`^/@([\w.-]*)/(video|photo)/(\d+)/?$`)

// shortCode is the path of a share link: vm.tiktok.com/CODE, vt.tiktok.com/CODE, tiktok.com/t/CODE.
var shortCode = regexp.MustCompile(`^/(?:t/)?\w+/?$`)

// Link is a TikTok post link, as far as the link itself says.
type Link struct {
	ID     string // the post's ID; "" for a short link until it is followed
	Handle string // the author's handle in the link, if any
	Photo  bool   // a /photo/ link
	Short  bool   // a share link (vm.tiktok.com/...), which redirects to the post
	raw    string
}

// ParseLink recognises a link to one TikTok post. ok is false for anything else (a profile,
// a hashtag, a sound, a live stream).
func ParseLink(link string) (l Link, ok bool) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return Link{}, false
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "www.tiktok.com", "tiktok.com", "m.tiktok.com":
		if m := postPath.FindStringSubmatch(u.Path); m != nil {
			return Link{ID: m[3], Handle: m[1], Photo: m[2] == "photo", raw: link}, true
		}
		if strings.HasPrefix(u.Path, "/t/") && shortCode.MatchString(u.Path) {
			return Link{Short: true, raw: link}, true
		}
	case "vm.tiktok.com", "vt.tiktok.com":
		if shortCode.MatchString(u.Path) && !strings.HasPrefix(u.Path, "/t/") {
			return Link{Short: true, raw: link}, true
		}
	}
	return Link{}, false
}

// Handles reports whether link is one TikTok post (or a share link to one), the kind of link
// Read answers.
func Handles(link string) bool {
	_, ok := ParseLink(link)
	return ok
}

// pageURL is the post page asked for on site. Photo posts answer at /video/ too, with the
// same item.
func (l Link) pageURL(site string) string {
	if l.Short {
		return l.raw
	}
	return site + "/@" + orUnderscore(l.Handle) + "/video/" + l.ID
}

func orUnderscore(handle string) string {
	if handle == "" {
		return "_"
	}
	return handle
}
