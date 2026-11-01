package tiktok

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math/rand/v2"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"yt-dlp-gui/internal/ytdlp"
)

// A profile is listed the way yt-dlp's tiktok:user lists it: the profile page gives the
// account (its secUid, and the header shown above the grid), then the unsigned
// /api/creator/item_list/ gives its posts, 15 a request, newest first. Measured on the same
// profile: the first 15 in ~0.8 s, where yt-dlp hands over nothing until it has read them all.
//
// What TikTok's list won't do (measured for TrueRip): more than 15 a request ("invalid count
// parameter"), oldest first (type=0 returns the same nine posts from any cursor), or anything
// for a private account.

// profilePath is a profile's path: /@handle, nothing after it.
var profilePath = regexp.MustCompile(`^/@([\w.-]{1,64})/?$`)

// ProfileHandle is the handle of a link to a TikTok profile (www., m. or the bare domain, any
// query), or "" for anything else.
func ProfileHandle(link string) string {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return ""
	}
	switch strings.ToLower(u.Hostname()) {
	case "www.tiktok.com", "tiktok.com", "m.tiktok.com":
	default:
		return ""
	}
	if m := profilePath.FindStringSubmatch(u.Path); m != nil {
		return m[1]
	}
	return ""
}

// IsProfile reports whether link is a TikTok profile, which ListProfile reads.
func IsProfile(link string) bool { return ProfileHandle(link) != "" }

const (
	pageSize = 15 // TikTok refuses more
	// quietStep is how far back the cursor goes after a page with nothing on it: the creator
	// posted nothing that week (yt-dlp's step).
	quietStep = 7 * 24 * time.Hour
	// epochMs is before TikTok existed; a cursor past it is the end.
	epochMs = 1_472_706_000_000
	// emptyPages bounds the quiet steps in a row, so a profile TikTok answers empty for ever
	// costs a few requests, not hundreds.
	emptyPages = 8
)

// Progress is told about a listing in parts as it is read (ytweb.Progress's shape): the first
// part holds the profile and the first page, each later one the next page.
type Progress func(part *ytdlp.ProbeResult)

// profileSite is TikTok's site; tests point it at a fake one.
type profileSite struct {
	base string
	tr   http.RoundTripper
	now  func() time.Time
}

// ListProfile lists a profile's posts, newest first: at most limit (0 for all), as yt-dlp's
// --playlist-end would. A private account, one with no posts, or anything unexpected fails, and
// callers list it with yt-dlp, which says why.
func ListProfile(ctx context.Context, o Options, link string, limit int, progress Progress) (*ytdlp.ProbeResult, error) {
	return profileSite{base: "https://www.tiktok.com", tr: transport(o), now: time.Now}.list(ctx, link, limit, progress)
}

func (s profileSite) list(ctx context.Context, link string, limit int, progress Progress) (*ytdlp.ProbeResult, error) {
	handle := ProfileHandle(link)
	if handle == "" {
		return nil, fmt.Errorf("not a TikTok profile link: %s", link)
	}
	jar, _ := cookiejar.New(nil)
	hc := &http.Client{Transport: s.tr, Jar: jar, Timeout: 30 * time.Second}
	page := s.base + "/@" + handle

	body, err := get(ctx, hc, page, "")
	if err != nil {
		return nil, err
	}
	top, secUID, err := profileTop(body, handle)
	if err != nil {
		return nil, err
	}

	var all []map[string]any
	parts := 0
	send := func(es []map[string]any) error {
		wrap := maps.Clone(top)
		if es == nil {
			es = []map[string]any{}
		}
		wrap["entries"] = es
		data, err := json.Marshal(wrap)
		if err != nil {
			return err
		}
		res, err := ytdlp.ParseProbe(data)
		if err != nil {
			return err
		}
		for i := range res.Playlist.Entries {
			res.Playlist.Entries[i].Index = len(all) - len(es) + i + 1
		}
		parts++
		if progress != nil {
			progress(res)
		}
		return nil
	}

	seen := map[string]bool{}
	cursor := s.now().UnixMilli()
	device := deviceID()
	retried, quiet := false, 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		items, more, err := s.itemList(ctx, hc, page, secUID, cursor, device)
		if err != nil {
			if parts == 0 {
				return nil, err
			}
			return nil, fmt.Errorf("reading the rest of the profile: %w", err)
		}
		var fresh []map[string]any
		for _, it := range items {
			id := str(it, "id")
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			fresh = append(fresh, flatEntry(it, handle))
		}
		if len(items) > 0 && len(fresh) == 0 {
			// The same page again: a device ID TikTok dislikes (yt-dlp#14031). One more try
			// with another, then it is the end rather than a loop.
			if !retried {
				retried, device = true, deviceID()
				continue
			}
			break
		}
		if limit > 0 && len(all)+len(fresh) > limit {
			fresh = fresh[:limit-len(all)]
		}
		all = append(all, fresh...)
		if len(fresh) > 0 || parts == 0 {
			if err := send(fresh); err != nil {
				return nil, err
			}
		}
		if limit > 0 && len(all) >= limit {
			break
		}
		old := cursor
		cursor = 0
		if len(items) > 0 {
			if t, ok := intOrNone(items[len(items)-1]["createTime"]); ok {
				cursor = t * 1000
			}
		}
		if len(items) == 0 {
			quiet++
		} else {
			quiet = 0
		}
		if cursor == 0 || cursor == old {
			cursor = old - quietStep.Milliseconds()
		}
		if cursor < epochMs || !more || quiet > emptyPages {
			break
		}
	}
	whole := maps.Clone(top)
	whole["entries"] = all
	data, err := json.Marshal(whole)
	if err != nil {
		return nil, err
	}
	return ytdlp.ParseProbe(data)
}

// profileTop reads the profile page: the listing without its entries, as yt-dlp's -J gives it
// (id = secUid, title = the handle), with the header the grid is drawn under.
func profileTop(body []byte, handle string) (map[string]any, string, error) {
	scope, err := pageScope(body)
	if err != nil {
		return nil, "", err
	}
	detail := obj(scope, "webapp.user-detail")
	info := obj(detail, "userInfo")
	user := obj(info, "user")
	if code := num(detail, "statusCode"); code != 0 || len(user) == 0 {
		msg := str(detail, "statusMsg")
		if msg == "" {
			msg = "no reason given"
		}
		return nil, "", &StatusError{Code: int(code), Message: msg}
	}
	if !strings.EqualFold(str(user, "uniqueId"), handle) {
		return nil, "", errors.New("TikTok's page carried a different account")
	}
	if b, _ := user["privateAccount"].(bool); b {
		return nil, "", errors.New("the account is private") // yt-dlp asks to sign in
	}
	secUID := str(user, "secUid")
	if secUID == "" {
		return nil, "", errors.New("TikTok's page didn't name the account's secUid")
	}
	stats := obj(info, "stats")
	if len(stats) == 0 {
		stats = obj(info, "statsV2")
	}
	likes := num(stats, "heartCount")
	if likes == 0 {
		likes = num(stats, "heart")
	}
	verified, _ := user["verified"].(bool)
	name := str(user, "uniqueId")
	return map[string]any{
		"_type": "playlist", "id": secUID, "title": name,
		"uploader": name, "uploader_id": strOrNumber(user, "id"),
		"channel": str(user, "nickname"), "channel_id": secUID,
		"webpage_url":   "https://www.tiktok.com/@" + name,
		"extractor":     "tiktok:user",
		"extractor_key": "TikTokUser",
		ytdlp.ProfileField: ytdlp.ChannelProfile{
			Name: str(user, "nickname"), Handle: name, Bio: str(user, "signature"), Verified: verified,
			// Medium is 720 px, drawn at under 100; Larger is the 1080 px original.
			Avatar:    firstStr(user, "avatarMedium", "avatarLarger", "avatarThumb"),
			Followers: num(stats, "followerCount"), Following: num(stats, "followingCount"),
			Likes: likes, Videos: num(stats, "videoCount"),
		},
		ytdlp.PortraitField: true,
	}, secUID, nil
}

// itemList asks for one page of a profile's posts before cursor (milliseconds).
func (s profileSite) itemList(ctx context.Context, hc *http.Client, profile, secUID string, cursor int64, device string) ([]map[string]any, bool, error) {
	q := url.Values{
		"aid": {"1988"}, "app_language": {"en"}, "app_name": {"tiktok_web"}, "browser_language": {"en-US"},
		"browser_name": {"Mozilla"}, "browser_online": {"true"}, "browser_platform": {"Win32"},
		"browser_version": {"5.0 (Windows)"}, "channel": {"tiktok_web"}, "cookie_enabled": {"true"},
		"count": {strconv.Itoa(pageSize)}, "cursor": {strconv.FormatInt(cursor, 10)}, "device_id": {device},
		"device_platform": {"web_pc"}, "focus_state": {"true"}, "from_page": {"user"}, "history_len": {"2"},
		"is_fullscreen": {"false"}, "is_page_visible": {"true"}, "language": {"en"}, "os": {"windows"},
		"priority_region": {""}, "referer": {""}, "region": {"US"}, "screen_height": {"1080"},
		"screen_width": {"1920"}, "secUid": {secUID}, "type": {"1"}, "tz_name": {"UTC"},
		"verifyFp": {"verify_" + hexChars(7)}, "webcast_language": {"en"},
	}
	body, err := get(ctx, hc, s.base+"/api/creator/item_list/?"+q.Encode(), profile)
	if err != nil {
		return nil, false, err
	}
	if len(body) == 0 {
		return nil, false, errors.New("TikTok's post list came back empty") // a refusal
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var page map[string]any
	if err := dec.Decode(&page); err != nil {
		return nil, false, fmt.Errorf("TikTok's post list didn't parse: %w", err)
	}
	if code := num(page, "statusCode"); code != 0 {
		return nil, false, &StatusError{Code: int(code), Message: firstStr(page, "statusMsg", "status_msg")}
	}
	var items []map[string]any
	for _, it := range arr(page, "itemList") {
		if m := asObj(it); str(m, "id") != "" {
			items = append(items, m)
		}
	}
	more, _ := page["hasMorePrevious"].(bool)
	return items, more, nil
}

// flatEntry is a post as yt-dlp's tiktok:user lists it (a url_result with the metadata of
// _parse_aweme_video_web), plus the grid-sized covers TikTok gives a list: its own covers are
// the full frame, 400-800 KB each.
func flatEntry(item map[string]any, handle string) map[string]any {
	p := &Post{item: item}
	link := "https://www.tiktok.com/@" + handle + "/video/" + p.ID()
	p.link, p.page = link, link
	e := p.common()
	e["_type"], e["ie_key"] = "url", "TikTok"
	e["url"] = e["webpage_url"] // a photo post's is its /photo/ link
	delete(e, "original_url")
	var thumbs []map[string]any
	zoom := obj(obj(item, "video"), "zoomCover")
	for _, size := range []string{"480", "720", "960"} {
		if u := urlOrNone(zoom[size]); u != "" {
			w, _ := strconv.Atoi(size)
			thumbs = append(thumbs, map[string]any{"id": "zoom" + size, "url": u, "width": w, "preference": -3})
		}
	}
	if old, ok := e["thumbnails"].([]map[string]any); ok {
		thumbs = append(thumbs, old...)
	}
	if len(thumbs) == 0 {
		// A photo post's first picture stands in for a cover it doesn't have.
		if pics := p.pictures(); len(pics) > 0 {
			thumbs = append(thumbs, map[string]any{"id": "picture", "url": pics[0].url})
		}
	}
	if len(thumbs) > 0 {
		e["thumbnails"] = thumbs
	}
	return e
}

// get fetches a TikTok page (referer "") or one of its API's answers (referer the page).
func get(ctx context.Context, hc *http.Client, link, referer string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if referer == "" {
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	} else {
		req.Header.Set("Accept", "*/*")
		req.Header.Set("Referer", referer)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TikTok answered %s", resp.Status)
	}
	if resp.Request.URL.Path == "/login" {
		return nil, ErrWithheld
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxPage))
}

// pageScope is a page's __DEFAULT_SCOPE__.
func pageScope(body []byte) (map[string]any, error) {
	m := rehydration.FindSubmatch(body)
	if m == nil {
		return nil, errors.New("TikTok's page carried no data")
	}
	dec := json.NewDecoder(bytes.NewReader(m[1]))
	dec.UseNumber() // IDs and counts as they are, never rounded through float64
	var data map[string]any
	if err := dec.Decode(&data); err != nil {
		return nil, fmt.Errorf("TikTok's page data didn't parse: %w", err)
	}
	return obj(data, "__DEFAULT_SCOPE__"), nil
}

// deviceID is a random web device ID, as yt-dlp's.
func deviceID() string { return strconv.FormatInt(rand.Int64N(9e18)+1e18, 10) }

func hexChars(n int) string {
	const digits = "0123456789abcdefABCDEF" // string.hexdigits, as yt-dlp draws from
	b := make([]byte, n)
	for i := range b {
		b[i] = digits[rand.IntN(len(digits))]
	}
	return string(b)
}
