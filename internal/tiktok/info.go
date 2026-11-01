package tiktok

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"yt-dlp-gui/internal/ytdlp"
)

// Info is the post as yt-dlp's -J would print it: a video's info dict, or for a photo post a
// playlist of its sound and its pictures (see the package comment).
func (p *Post) Info() ([]byte, error) {
	if p.Photo() {
		return json.Marshal(p.photoInfo())
	}
	info := p.common()
	formats := p.videoFormats()
	if a := p.audioFormat(); a != nil {
		formats = append(formats, a)
	}
	info["formats"] = p.finish(formats, number(info["duration"]))
	if subs := p.subtitles(); len(subs) > 0 {
		info["subtitles"] = subs
	}
	return json.Marshal(info)
}

// common is the metadata of yt-dlp's _parse_aweme_video_web, plus what yt-dlp's core adds.
func (p *Post) common() map[string]any {
	it := p.item
	id := p.ID()
	author := obj(it, "author")
	if len(author) == 0 {
		author = obj(it, "authorInfo")
	}
	info := map[string]any{
		"id":                   id,
		"extractor":            "TikTok",
		"extractor_key":        "TikTok",
		"webpage_url_basename": id,
		"webpage_url_domain":   "tiktok.com",
		"original_url":         p.link,
	}
	set := func(k, v string) {
		if v != "" {
			info[k] = v
		}
	}
	set("channel", str(author, "nickname"))
	set("channel_id", firstStr(author, "authorSecId", "secUid"))
	uploader := firstStr(author, "uniqueId", "author")
	set("uploader", uploader)
	uploaderID := strOrNumber(author, "authorId", "uid", "id")
	set("uploader_id", uploaderID)
	if cid := firstStr(author, "authorSecId", "secUid"); cid != "" {
		info["channel_url"] = "https://www.tiktok.com/@" + cid
	}
	if u := firstNonEmpty(uploader, uploaderID); u != "" {
		info["uploader_url"] = "https://www.tiktok.com/@" + u
	}

	music := obj(it, "music")
	set("track", str(music, "title"))
	set("album", str(music, "album"))
	if a := str(music, "authorName"); a != "" {
		info["artists"] = artistSplit.Split(a, -1)
	}
	if d, ok := intOrNone(music["duration"]); ok {
		info["duration"] = d
	}
	// Photo posts have a video duration of 0; the sound's stands.
	if d, ok := intOrNone(obj(it, "video")["duration"]); ok && d != 0 {
		info["duration"] = d
	}

	desc, _ := it["desc"].(string)
	title := truncate(desc, 72)
	if title == "" {
		title = "TikTok video #" + id // yt-dlp's own fallback for a post without a caption
	}
	info["title"] = title
	if _, ok := it["desc"].(string); ok {
		info["description"] = desc
	}
	if ts, ok := intOrNone(it["createTime"]); ok {
		info["timestamp"] = ts
		if ts > 0 {
			// yt-dlp's core adds it; the UI shows it.
			info["upload_date"] = time.Unix(ts, 0).UTC().Format("20060102")
		}
	}
	stats := obj(it, "stats")
	for k, field := range map[string]string{
		"view_count": "playCount", "like_count": "diggCount", "repost_count": "shareCount",
		"comment_count": "commentCount", "save_count": "collectCount",
	} {
		if n, ok := intOrNone(stats[field]); ok {
			info[k] = n
		}
	}

	var thumbs []map[string]any
	video := obj(it, "video")
	for _, cid := range []string{"thumbnail", "cover", "dynamicCover", "originCover"} {
		for _, src := range []map[string]any{it, video} {
			if u := urlOrNone(src[cid]); u != "" {
				pref := -1
				if cid == "dynamicCover" {
					pref = -2
				}
				thumbs = append(thumbs, map[string]any{"id": cid, "url": u, "preference": pref})
			}
		}
	}
	if len(thumbs) > 0 {
		info["thumbnails"] = thumbs
	}

	kind := "video"
	if p.Photo() {
		kind = "photo"
	}
	info["webpage_url"] = "https://www.tiktok.com/@" + orUnderscore(uploader) + "/" + kind + "/" + id
	// yt-dlp sends the post page as Referer (its _create_url); the page's cookies go with each
	// format (finish).
	info["http_headers"] = map[string]any{"Referer": "https://www.tiktok.com/@" + orUnderscore(p.linkHandle()) + "/video/" + id}
	return info
}

func (p *Post) linkHandle() string {
	if l, ok := ParseLink(p.page); ok {
		return l.Handle
	}
	return ""
}

// urlKey is a rendition's UrlKey: v<...>_<codec>_<res>p_<bitrate>, yt-dlp's _parse_url_key.
var urlKey = regexp.MustCompile(`v[^_]+_(([^_]+)_(\d+p)_(\d+))`)

// qualities is yt-dlp's TikTokBaseIE.QUALITIES: a resolution's place in it, or -1.
var qualities = []string{"360p", "540p", "720p", "1080p"}

func quality(res string) int {
	for i, q := range qualities {
		if q == res {
			return i
		}
	}
	return -1
}

// videoFormats is yt-dlp's _extract_web_formats without the sound: every URL of every rung of
// bitrateInfo, the player's file ("play") and the watermarked one ("download").
func (p *Post) videoFormats() []map[string]any {
	video := obj(p.item, "video")
	playW, wOK := intOrNone(video["width"])
	playH, hOK := intOrNone(video["height"])
	ratio := 0.5625
	if wOK && hOK && playH != 0 && playW != 0 {
		ratio = float64(playW) / float64(playH)
	}
	common := func() map[string]any {
		return map[string]any{"ext": "mp4", "vcodec": "h264", "acodec": "aac"}
	}
	var formats []map[string]any
	for _, g := range arr(video, "bitrateInfo") {
		gear, _ := g.(map[string]any)
		addr := obj(gear, "PlayAddr")
		urls := arr(addr, "UrlList")
		if len(urls) == 0 {
			continue
		}
		info := map[string]any{}
		var res string
		if m := urlKey.FindStringSubmatch(str(addr, "UrlKey")); m != nil {
			codec := m[2]
			if codec == "bytevc1" {
				codec = "h265"
			}
			res = m[3]
			info["format_id"] = m[1]
			info["vcodec"] = codec
			if br, err := strconv.ParseInt(m[4], 10, 64); err == nil && br/1000 != 0 {
				info["tbr"] = br / 1000
			}
			info["quality"] = quality(res)
		}
		// bytevc2 is ByteDance's own H.266, which nothing plays yet.
		if info["vcodec"] == "bytevc2" {
			info["format_note"] = "UNPLAYABLE"
			info["preference"] = -100
		} else {
			info["preference"] = -1
		}
		if size, ok := intOrNone(addr["DataSize"]); ok {
			info["filesize"] = size
		}
		if res != "" {
			dim, _ := strconv.ParseInt(strings.TrimSuffix(res, "p"), 10, 64)
			if dim == 540 {
				dim = 576 // TikTok's "540p" is 576 high
			}
			if dim != 0 {
				if ratio < 1 { // portrait: the resolution is the width
					y := int64(float64(dim) / ratio)
					info["width"], info["height"] = dim, y-y%2
				} else {
					x := int64(float64(dim) * ratio)
					info["width"], info["height"] = x+x%2, dim
				}
			}
		}
		for _, u := range urls {
			s := urlOrNone(u)
			if s == "" {
				continue
			}
			f := common()
			for k, v := range info {
				f[k] = v
			}
			f["url"] = s
			if pu, err := url.Parse(s); err == nil && strings.HasSuffix(pu.Path, "/media-video-hvc1/") {
				f["acodec"] = "none" // some H.265 links are video only (yt-dlp#16622)
			}
			formats = append(formats, f)
		}
	}

	// The player's file. yt-dlp gives it the page's width and height and the quality of a rung
	// as wide; its size comes from PlayAddrStruct when that describes the same file.
	var playQuality any
	for _, f := range formats {
		if wOK && f["width"] == playW {
			playQuality = f["quality"]
			break
		}
	}
	var plays []string
	switch pa := video["playAddr"].(type) {
	case string:
		plays = append(plays, pa)
	case []any:
		for _, e := range pa {
			plays = append(plays, str(asObj(e), "src"))
		}
	}
	for _, pu := range plays {
		s := urlOrNone(pu)
		if s == "" {
			continue
		}
		f := common()
		f["format_id"] = "play"
		f["url"] = s
		if wOK {
			f["width"] = playW
		}
		if hOK {
			f["height"] = playH
		}
		if playQuality != nil {
			f["quality"] = playQuality
		}
		if size := playSize(video, s); size > 0 {
			f["filesize"] = size
		}
		formats = append(formats, f)
	}

	for _, du := range []string{str(video, "downloadAddr"), str(obj(video, "download"), "url")} {
		if s := urlOrNone(du); s != "" {
			f := common()
			f["format_id"] = "download"
			f["url"] = s
			f["format_note"] = "watermarked"
			f["preference"] = -2
			formats = append(formats, f)
		}
	}
	return dedupe(formats)
}

// playSize is the size PlayAddrStruct gives the player's file at link: its stored object is
// the same on every mirror and signing (/video/tos/...), so the paths are compared.
func playSize(video map[string]any, link string) int64 {
	st := obj(video, "PlayAddrStruct")
	size, ok := intOrNone(st["DataSize"])
	if !ok || size <= 0 {
		return 0
	}
	want := objectPath(link)
	for _, u := range arr(st, "UrlList") {
		if s, _ := u.(string); s != "" && objectPath(s) == want {
			return size
		}
	}
	return 0
}

// objectPath is what a CDN link names apart from its signature, or the link itself when it
// isn't a stored object (TikTok's generic /aweme/v1/play/ door, the same path for every file).
func objectPath(link string) string {
	u, err := url.Parse(link)
	if err != nil || !strings.Contains(u.Path, "/video/tos/") {
		return link
	}
	return u.Path
}

// dedupe drops formats whose URL came before (yt-dlp's _remove_duplicate_formats).
func dedupe(formats []map[string]any) []map[string]any {
	seen := map[string]bool{}
	out := formats[:0]
	for _, f := range formats {
		u := f["url"].(string)
		if !seen[u] {
			seen[u] = true
			out = append(out, f)
		}
	}
	return out
}

// audioFormat is the post's sound, as yt-dlp's "audio" format.
func (p *Post) audioFormat() map[string]any {
	u := urlOrNone(obj(p.item, "music")["playUrl"])
	if u == "" {
		return nil
	}
	ext := "m4a"
	if pu, err := url.Parse(u); err == nil {
		if mt := pu.Query()["mime_type"]; len(mt) > 0 {
			if e := mimeExt(strings.ReplaceAll(mt[len(mt)-1], "_", "/")); e != "" {
				ext = e
			}
		}
	}
	acodec := ext
	if ext == "m4a" {
		acodec = "aac"
	}
	return map[string]any{"format_id": "audio", "url": u, "ext": ext, "acodec": acodec, "vcodec": "none"}
}

// mimeExt is yt-dlp's mimetype2ext for the types a sound comes as.
func mimeExt(mt string) string {
	_, sub, ok := strings.Cut(strings.ToLower(strings.TrimSpace(mt)), "/")
	if !ok {
		return ""
	}
	sub, _, _ = strings.Cut(sub, ";")
	switch sub {
	case "mpeg", "mp3":
		return "mp3"
	case "mp4", "x-m4a", "m4a":
		return "m4a"
	case "aac", "x-aac":
		return "aac"
	case "ogg", "opus", "webm", "flac", "wav":
		return sub
	case "x-wav", "wave", "vnd.wave":
		return "wav"
	}
	return ""
}

// finish is what yt-dlp's core does to an extractor's formats that the UI reads (resolution,
// an approximate size from the bitrate) and what a download needs (the page's cookies, scoped
// to tiktok.com, and unique IDs: duplicates get -0, -1... in the order they came).
func (p *Post) finish(formats []map[string]any, duration float64) []map[string]any {
	var kept []map[string]any
	for _, f := range formats {
		// TikTok's own www host serves a broken copy (yt-dlp#11034).
		if u, err := url.Parse(f["url"].(string)); err == nil && u.Hostname() == "www.tiktok.com" {
			continue
		}
		kept = append(kept, f)
	}
	count := map[string]int{}
	for _, f := range kept {
		if id, ok := f["format_id"].(string); ok {
			count[id]++
		}
	}
	next := map[string]int{}
	cookies := p.cookieField()
	for i, f := range kept {
		id, ok := f["format_id"].(string)
		if !ok {
			id = strconv.Itoa(i)
		}
		id = formatIDUnsafe.ReplaceAllString(id, "_")
		if count[id] > 1 {
			f["format_id"] = fmt.Sprintf("%s-%d", id, next[id])
			next[id]++
		} else {
			f["format_id"] = id
		}
		w, wok := f["width"].(int64)
		h, hok := f["height"].(int64)
		switch {
		case f["vcodec"] == "none" && f["acodec"] != "none":
			f["resolution"] = "audio only"
		case wok && hok:
			f["resolution"] = fmt.Sprintf("%dx%d", w, h)
		case hok:
			f["resolution"] = fmt.Sprintf("%dp", h)
		}
		if f["vcodec"] != "none" {
			f["dynamic_range"] = "SDR"
		}
		if _, ok := f["filesize"]; !ok {
			if tbr, ok := f["tbr"].(int64); ok && duration > 0 {
				f["filesize_approx"] = int64(duration * float64(tbr) * 1000 / 8)
			}
		}
		if cookies != "" {
			f["cookies"] = cookies
		}
		f["protocol"] = "https"
	}
	return kept
}

// formatIDUnsafe are the characters yt-dlp's core replaces in a format ID (they mean something
// in a format selector).
var formatIDUnsafe = regexp.MustCompile(`[\s,/+\[\]()]`)

// cookieField is the page's cookies in the Set-Cookie-like form yt-dlp's -J gives each format,
// which --load-info-json loads scoped to their domain.
func (p *Post) cookieField() string {
	var parts []string
	for _, c := range p.cookies {
		if c.Name == "" || strings.ContainsAny(c.Value, ";\r\n") {
			continue
		}
		parts = append(parts, c.Name+"="+c.Value, "Domain=.tiktok.com", "Path=/", "Secure")
	}
	return strings.Join(parts, "; ")
}

// subtitles are the page's subtitleInfos as yt-dlp's subtitles (TikTok's speech recognition
// included: yt-dlp doesn't tell it apart either). yt-dlp reads them only when subtitles are
// asked for; here they are listed, so they can be picked.
func (p *Post) subtitles() map[string][]map[string]any {
	ext := map[string]string{"creator_caption": "json", "srt": "srt", "webvtt": "vtt"}
	rank := map[string]int{"json": 0, "srt": 1, "vtt": 2}
	out := map[string][]map[string]any{}
	for _, s := range arr(obj(p.item, "video"), "subtitleInfos") {
		t := asObj(s)
		u := str(t, "Url")
		if u == "" {
			continue
		}
		lang := str(t, "LanguageCodeName")
		if lang == "" {
			lang = "en"
		}
		e := ext[str(t, "Format")]
		sub := map[string]any{"url": u}
		if e != "" {
			sub["ext"] = e
		}
		out[lang] = append(out[lang], sub)
	}
	for _, list := range out {
		// Least preferred first, as yt-dlp sorts them.
		for i := 1; i < len(list); i++ {
			for j := i; j > 0 && rank[str(list[j], "ext")] < rank[str(list[j-1], "ext")]; j-- {
				list[j], list[j-1] = list[j-1], list[j]
			}
		}
	}
	return out
}

// picture is one of a photo post's pictures.
type picture struct {
	url           string
	width, height int64
}

func (p *Post) pictures() []picture {
	var out []picture
	for _, e := range arr(obj(p.item, "imagePost"), "images") {
		img := asObj(e)
		for _, u := range arr(obj(img, "imageURL"), "urlList") {
			if s := urlOrNone(u); s != "" {
				w, _ := intOrNone(img["imageWidth"])
				h, _ := intOrNone(img["imageHeight"])
				out = append(out, picture{url: s, width: w, height: h})
				break
			}
		}
	}
	return out
}

// photoInfo is a photo post as a playlist: its sound first (so the audio presets can take
// just that, --playlist-items=1), then its pictures. The top level also carries the sound's
// format and photo_count for ParseProbe, which reads it as one item to download.
func (p *Post) photoInfo() map[string]any {
	top := p.common()
	pics := p.pictures()
	var entries []map[string]any
	meta := func() map[string]any {
		e := cloneMap(top)
		// The title names the post's folder: Windows can't end a folder name with a dot, and
		// yt-dlp's cut of a long caption ends with "...".
		if t := strings.TrimRight(str(e, "title"), ". "); t != "" {
			e["title"] = t
		}
		return e
	}
	if a := p.audioFormat(); a != nil {
		sound := meta()
		sound["formats"] = p.finish([]map[string]any{a}, 0)
		sound[ytdlp.PhotoFileField] = "Sound"
		entries = append(entries, sound)
		top["formats"] = p.finish([]map[string]any{cloneMap(a)}, 0)
	}
	width := max(2, len(strconv.Itoa(len(pics))))
	for i, pic := range pics {
		e := meta()
		n := i + 1
		e["id"] = fmt.Sprintf("%s-%d", p.ID(), n)
		e[ytdlp.PhotoFileField] = fmt.Sprintf("%0*d", width, n)
		delete(e, "duration")
		delete(e, "thumbnails")
		f := map[string]any{
			"format_id": "picture", "url": pic.url, "ext": pictureExt(pic.url),
			"vcodec": "none", "acodec": "none",
		}
		if pic.width > 0 && pic.height > 0 {
			f["width"], f["height"] = pic.width, pic.height
		}
		e["formats"] = p.finish([]map[string]any{f}, 0)
		entries = append(entries, e)
	}
	// A playlist, not a multi_video: yt-dlp joins a multi_video's parts into one file.
	top["_type"] = "playlist"
	top["entries"] = entries
	top[ytdlp.PhotoCountField] = len(pics)
	return top
}

// pictureExt is the type a picture's link names (every one seen ends ~tplv-photomode-image.jpeg).
func pictureExt(link string) string {
	path := strings.ToLower(link)
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	for _, e := range []string{"jpeg", "jpg", "png", "webp", "heic", "avif", "gif"} {
		if strings.HasSuffix(path, "."+e) {
			if e == "jpeg" {
				return "jpg"
			}
			return e
		}
	}
	return "jpg"
}

// ---- reading JSON nobody promised would be well formed ----
//
// Each accessor returns the type it promises or an empty one, so a page whose fields arrive as
// strings, nulls or lists gives a thinner answer, never a panic.

func asObj(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func obj(m map[string]any, k string) map[string]any { return asObj(m[k]) }

func arr(m map[string]any, k string) []any {
	a, _ := m[k].([]any)
	return a
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

func firstStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := str(m, k); s != "" {
			return s
		}
	}
	return ""
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// strOrNumber is the first of keys that is a string or a number, as a string (yt-dlp's
// str_or_none).
func strOrNumber(m map[string]any, keys ...string) string {
	for _, k := range keys {
		switch v := m[k].(type) {
		case string:
			if v != "" {
				return v
			}
		case json.Number:
			return v.String()
		}
	}
	return ""
}

// num is m[k] as a number, 0 when it isn't one.
func num(m map[string]any, k string) int64 {
	n, _ := intOrNone(m[k])
	return n
}

// intOrNone is yt-dlp's int_or_none: a number, or a string holding one.
func intOrNone(v any) (int64, bool) {
	switch v := v.(type) {
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n, true
		}
		if f, err := v.Float64(); err == nil && !math.IsInf(f, 0) {
			return int64(f), true
		}
	case string:
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			return n, true
		}
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	}
	return 0, false
}

func number(v any) float64 {
	n, _ := intOrNone(v)
	return float64(n)
}

// urlOrNone is yt-dlp's url_or_none for the links a page gives, made absolute
// (_proto_relative_url): "" for anything else.
func urlOrNone(v any) string {
	s, _ := v.(string)
	s = strings.TrimSpace(s)
	switch {
	case strings.HasPrefix(s, "//"):
		return "https:" + s
	case strings.HasPrefix(s, "https://"), strings.HasPrefix(s, "http://"):
		return s
	}
	return ""
}

// artistSplit is how yt-dlp splits a sound's authorName into artists.
var artistSplit = regexp.MustCompile(`, | & `)

// truncate is yt-dlp's truncate_string(s, left): at most left characters, "..." marking a cut.
func truncate(s string, left int) string {
	r := []rune(s)
	if len(r) <= left {
		return s
	}
	return string(r[:left-3]) + "..."
}

func cloneMap(m map[string]any) map[string]any {
	c := make(map[string]any, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}
