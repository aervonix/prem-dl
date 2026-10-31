package ytweb

import (
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The parser mirrors yt-dlp's YouTube search extractor (yt_dlp/extractor/youtube/_tab.py:
// _extract_entries, _extract_video, _extract_channel_renderer, _extract_lockup_view_model,
// _grid_entries, _shelf_entries), field by field, so its entries are the JSON yt-dlp prints for
// `-J --flat-playlist --extractor-args youtubetab:approximate_date`. The fixtures in testdata
// hold real pages next to what yt-dlp made of them, and the tests compare the two.

// entry is one result, named and shaped like yt-dlp's flat entries.
type entry struct {
	Type       string  `json:"_type"`
	IEKey      string  `json:"ie_key,omitempty"`
	ID         string  `json:"id,omitempty"`
	URL        string  `json:"url"`
	Title      string  `json:"title,omitempty"`
	Uploader   string  `json:"uploader,omitempty"`
	Thumbnails []thumb `json:"thumbnails,omitempty"`
	Duration   float64 `json:"duration,omitempty"`
	ViewCount  *int64  `json:"view_count,omitempty"`
	Timestamp  int64   `json:"timestamp,omitempty"`
	// ReleaseTimestamp is when a scheduled stream or premiere starts, as yt-dlp gives it.
	ReleaseTimestamp int64  `json:"release_timestamp,omitempty"`
	LiveStatus       string `json:"live_status,omitempty"`
	Availability     string `json:"availability,omitempty"`
}

type thumb struct {
	URL    string `json:"url"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

// page is one answer of the search or browse API: its results, the token for the next page
// ("" at the end) and the visitor ID YouTube gave this session.
type page struct {
	entries []entry
	next    string
	visitor string
}

var errLayout = errors.New("YouTube's answer isn't laid out as expected")

// parseSearch reads a search API answer (_search_results).
func parseSearch(data []byte, x extractor) (page, error) {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return page{}, err
	}
	p := page{visitor: str(root, "responseContext", "visitorData")}
	sections := list(root, "contents", "twoColumnSearchResultsRenderer", "primaryContents", "sectionListRenderer", "contents")
	if sections == nil {
		sections = list(root, "onResponseReceivedCommands", 0, "appendContinuationItemsAction", "continuationItems")
	}
	if sections == nil {
		// The last page answers with no items at all; anything else has moved.
		if _, ok := root["onResponseReceivedCommands"]; ok {
			return p, nil
		}
		return page{}, errLayout
	}
	p.next = x.entries(map[string]any{"contents": sections}, &p.entries)
	return p, nil
}

// parseContinuation reads a browse API answer to a continuation token (the loop in _entries):
// the first item's kind says how the items are laid out.
func parseContinuation(root map[string]any, x extractor) page {
	p := page{visitor: str(root, "responseContext", "visitorData")}
	var items []any
	for _, key := range []string{"onResponseReceivedActions", "onResponseReceivedEndpoints"} {
		for _, a := range list(root, key) {
			if items = list(a, "appendContinuationItemsAction", "continuationItems"); items != nil {
				break
			}
		}
		if items != nil {
			break
		}
	}
	first, _ := get(items, 0).(map[string]any)
	for _, key := range sortedKeys(first) {
		var parent map[string]any
		switch key {
		case "videoRenderer", "gridPlaylistRenderer", "gridVideoRenderer", "gridChannelRenderer", "lockupViewModel":
			parent = map[string]any{"items": items}
			x.grid(items, &p.entries)
		case "playlistVideoRenderer":
			parent = map[string]any{"contents": items}
			x.playlistVideos(items, &p.entries)
		case "itemSectionRenderer", "richItemRenderer":
			parent = map[string]any{"contents": items}
			p.next = x.entries(parent, &p.entries)
		default:
			continue
		}
		if p.next == "" {
			p.next = continuationOf(parent)
		}
	}
	if p.next == "" {
		// Only a continuation came back: follow it.
		p.next = continuationOf(map[string]any{"contents": []any{first}})
	}
	return p
}

// continuationOf finds the next page's token under a renderer (_extract_continuation).
func continuationOf(r map[string]any) string {
	if t := str(r, "continuations", 0, "nextContinuationData", "continuation"); t != "" {
		return t
	}
	if t := str(r, "continuation", "reloadContinuationData", "continuation"); t != "" {
		return t
	}
	for _, key := range []string{"contents", "items", "rows", "subThreads"} {
		for _, c := range list(r, key) {
			for _, ep := range []map[string]any{
				obj(c, "continuationItemRenderer", "continuationEndpoint"),
				obj(c, "continuationItemRenderer", "button", "buttonRenderer", "command"),
				obj(c, "continuationItemViewModel", "continuationCommand", "innertubeCommand"),
			} {
				if ep == nil {
					continue
				}
				for _, cmd := range list(ep, "commandExecutorCommand", "commands") {
					if t := str(cmd, "continuationCommand", "token"); t != "" {
						return t
					}
				}
				if t := str(ep, "continuationCommand", "token"); t != "" {
					return t
				}
			}
		}
	}
	return ""
}

// extractor turns renderers into entries.
type extractor struct {
	now time.Time
	// dates turns "3 years ago" into a timestamp, as yt-dlp's youtubetab:approximate_date
	// does; the app asks for it everywhere.
	dates bool
}

func (x extractor) timestamp(text string) int64 {
	if !x.dates {
		return 0
	}
	return parseTimeText(text, x.now)
}

// entries reads the results under a parent renderer's contents and returns the next page's
// token (_extract_entries).
func (x extractor) entries(parent map[string]any, out *[]entry) (next string) {
	for _, c := range list(parent, "contents") {
		content, ok := c.(map[string]any)
		if !ok {
			continue
		}
		section := obj(content, "itemSectionRenderer")
		if section == nil {
			if rich := obj(content, "richItemRenderer"); rich != nil {
				x.rich(rich, out)
				next = continuationOf(parent)
			}
			continue
		}
		for _, sc := range list(section, "contents") {
			item, ok := sc.(map[string]any)
			if !ok {
				continue
			}
			if r, n, ok := x.item(item, out); ok {
				next = n
				if n == "" {
					next = continuationOf(r)
				}
			}
		}
		if next == "" {
			next = continuationOf(section)
		}
	}
	if next == "" {
		next = continuationOf(parent)
	}
	return next
}

// item reads one entry of an itemSectionRenderer: the first renderer yt-dlp knows wins. It
// returns that renderer, and the token a nested grid found.
func (x extractor) item(item map[string]any, out *[]entry) (renderer map[string]any, next string, ok bool) {
	for _, key := range sortedKeys(item) {
		r, _ := item[key].(map[string]any)
		if r == nil {
			continue
		}
		switch key {
		case "videoRenderer":
			if str(r, "videoId") != "" {
				*out = append(*out, x.video(r))
			}
		case "playlistRenderer", "channelRenderer":
			x.grid([]any{map[string]any{key: r}}, out)
		case "gridRenderer", "reelShelfRenderer":
			x.grid(list(r, "items"), out)
		case "shelfRenderer":
			x.shelf(r, out)
		case "playlistVideoListRenderer":
			x.playlistVideos(list(r, "contents"), out)
		case "richGridRenderer":
			next = x.entries(r, out)
		case "lockupViewModel":
			if e, ok := x.lockup(r); ok {
				*out = append(*out, e)
			}
		case "hashtagTileRenderer":
			if u := absURL(str(r, "onTapCommand", "commandMetadata", "webCommandMetadata", "url")); u != "" {
				*out = append(*out, entry{Type: "url", IEKey: "YoutubeTab", URL: u, Title: text(r["hashtag"])})
			}
		default:
			continue
		}
		return r, next, true
	}
	return nil, "", false
}

// rich reads a richItemRenderer, which channel tabs lay their videos out in (_rich_entries).
func (x extractor) rich(r map[string]any, out *[]entry) {
	if lvm := obj(r, "content", "lockupViewModel"); lvm != nil {
		if e, ok := x.lockup(lvm); ok {
			*out = append(*out, e)
		}
		return
	}
	var inner map[string]any
	for _, key := range []string{"videoRenderer", "reelItemRenderer", "playlistRenderer", "shortsLockupViewModel"} {
		if inner = obj(r, "content", key); inner != nil {
			break
		}
	}
	switch {
	case inner == nil:
	case str(inner, "videoId") != "":
		*out = append(*out, x.video(inner))
	case str(inner, "playlistId") != "":
		id := str(inner, "playlistId")
		*out = append(*out, entry{Type: "url", IEKey: "YoutubeTab", ID: id,
			URL: "https://www.youtube.com/playlist?list=" + id, Title: text(inner["title"])})
	case str(inner, "entityId") != "":
		if e, ok := short(inner); ok {
			*out = append(*out, e)
		}
	}
}

var shortTitleRe = regexp.MustCompile(`^(.+), (?:[\d,.]+(?:[KM]| million)?|No) views? - play Short$`)

// short reads a shortsLockupViewModel, a Short on a channel's Shorts tab.
func short(vm map[string]any) (entry, bool) {
	id := str(vm, "onTap", "innertubeCommand", "reelWatchEndpoint", "videoId")
	if id == "" {
		return entry{}, false
	}
	e := entry{Type: "url", IEKey: "Youtube", ID: id, URL: "https://www.youtube.com/shorts/" + id,
		Thumbnails: thumbnails(list(vm, "thumbnailViewModel", "thumbnailViewModel", "image", "sources"))}
	e.Title = str(vm, "overlayMetadata", "primaryText", "content")
	if e.Title == "" {
		if m := shortTitleRe.FindStringSubmatch(str(vm, "accessibilityText")); m != nil {
			e.Title = m[1]
		}
	}
	if n, ok := parseCount(str(vm, "overlayMetadata", "secondaryText", "content")); ok {
		e.ViewCount = &n
	}
	return e, true
}

// playlistVideos reads a playlist's videos (_playlist_entries).
func (x extractor) playlistVideos(items []any, out *[]entry) {
	for _, it := range items {
		r := obj(it, "playlistVideoRenderer")
		if r == nil {
			r = obj(it, "playlistPanelVideoRenderer")
		}
		if r != nil && str(r, "videoId") != "" {
			*out = append(*out, x.video(r))
		}
	}
}

// sortedKeys gives a renderer's keys in a fixed order. A YouTube item holds one renderer, so
// the order only matters for the odd one that holds more.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	if len(keys) > 1 {
		slices.Sort(keys)
	}
	return keys
}

// grid reads gridRenderer-style items (_grid_entries).
func (x extractor) grid(items []any, out *[]entry) {
	for _, it := range items {
		item, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if lvm := obj(item, "lockupViewModel"); lvm != nil {
			if e, ok := x.lockup(lvm); ok {
				*out = append(*out, e)
			}
			continue
		}
		r := basicRenderer(item)
		if r == nil {
			continue
		}
		title := text(r["title"])
		switch {
		case str(r, "playlistId") != "":
			id := str(r, "playlistId")
			*out = append(*out, entry{Type: "url", IEKey: "YoutubeTab", ID: id,
				URL: "https://www.youtube.com/playlist?list=" + id, Title: title})
		case str(r, "videoId") != "":
			*out = append(*out, x.video(r))
		case str(r, "channelId") != "":
			*out = append(*out, x.channel(r))
		default:
			if e, ok := linkEntry(str(r, "navigationEndpoint", "commandMetadata", "webCommandMetadata", "url"), title); ok {
				*out = append(*out, e)
			}
		}
	}
}

func basicRenderer(item map[string]any) map[string]any {
	for _, key := range sortedKeys(item) {
		r, ok := item[key].(map[string]any)
		if !ok {
			continue
		}
		switch key {
		case "playlistRenderer", "videoRenderer", "channelRenderer", "showRenderer", "reelItemRenderer":
			return r
		}
		if strings.HasPrefix(key, "grid") && strings.HasSuffix(key, "Renderer") {
			return r
		}
	}
	return nil
}

// linkEntry is a result known only by its link (yt-dlp's "generic endpoint URL support").
func linkEntry(link, title string) (entry, bool) {
	u, err := url.Parse(absURL(link))
	if link == "" || err != nil {
		return entry{}, false
	}
	q := u.Query()
	switch {
	case u.Path == "/watch" && q.Get("v") != "" && q.Get("list") == "":
		return entry{Type: "url", IEKey: "Youtube", ID: q.Get("v"), URL: u.String(), Title: title}, true
	case q.Get("list") != "":
		return entry{Type: "url", IEKey: "YoutubeTab", ID: q.Get("list"), URL: u.String(), Title: title}, true
	case strings.HasPrefix(u.Path, "/@") || strings.HasPrefix(u.Path, "/channel/"):
		return entry{Type: "url", IEKey: "YoutubeTab", URL: u.String(), Title: title}, true
	}
	return entry{}, false
}

// shelf reads a shelfRenderer: a link to the shelf's own page, then what it shows.
func (x extractor) shelf(r map[string]any, out *[]entry) {
	if u := absURL(str(r, "endpoint", "commandMetadata", "webCommandMetadata", "url")); u != "" {
		*out = append(*out, entry{Type: "url", URL: u, Title: text(r["title"])})
	}
	content := obj(r, "content")
	if g := obj(content, "gridRenderer"); g != nil {
		x.grid(list(g, "items"), out)
	} else if g := obj(content, "expandedShelfContentsRenderer"); g != nil {
		x.grid(list(g, "items"), out)
	}
}

// video reads a videoRenderer (_extract_video).
func (x extractor) video(r map[string]any) entry {
	id := str(r, "videoId")
	reel := obj(r, "navigationEndpoint", "reelWatchEndpoint", "overlay", "reelPlayerOverlayRenderer",
		"reelPlayerHeaderSupportedRenderers", "reelPlayerHeaderRenderer")

	title := firstText(r, "title", "headline")
	if title == "" {
		title = text(get(reel, "reelTitleText"))
	}
	var overlays []map[string]any
	for _, o := range list(r, "thumbnailOverlays") {
		if ts := obj(o, "thumbnailOverlayTimeStatusRenderer"); ts != nil {
			overlays = append(overlays, ts)
		}
	}

	duration, ok := float64(0), false
	if s, isStr := r["lengthSeconds"].(string); isStr {
		if n, err := strconv.Atoi(s); err == nil {
			duration, ok = float64(n), true
		}
	} else if n, isNum := r["lengthSeconds"].(float64); isNum {
		duration, ok = n, true
	}
	if !ok {
		t := text(r["lengthText"])
		for _, o := range overlays {
			if t != "" {
				break
			}
			t = text(o["text"])
		}
		duration, ok = parseDuration(t)
	}
	if !ok {
		duration, _ = durationFromLabel(str(r, "title", "accessibility", "accessibilityData", "label"))
	}

	overlayStyle := ""
	for _, o := range overlays {
		if s, ok := o["style"].(string); ok {
			overlayStyle = s
			break
		}
	}
	badges := extractBadges(list(r, "badges"))
	nav := absURL(str(r, "navigationEndpoint", "commandMetadata", "webCommandMetadata", "url"))
	link := "https://www.youtube.com/watch?v=" + id
	if overlayStyle == "SHORTS" || strings.Contains(nav, "/shorts/") {
		link = "https://www.youtube.com/shorts/" + id
	}

	timeText := firstText(r, "publishedTimeText", "videoInfo")
	if timeText == "" {
		timeText = text(get(reel, "timestampText"))
	}
	starts, upcoming := toInt(get(r, "upcomingEventData", "startTime"))

	live := ""
	switch {
	case upcoming:
		live = "is_upcoming"
	case strings.Contains(strings.ToLower(timeText), "streamed"):
		live = "was_live"
	case overlayStyle == "LIVE" || badges[badgeLiveNow]:
		live = "is_live"
	}

	e := entry{
		Type: "url", IEKey: "Youtube", ID: id, URL: link, Title: title, Duration: duration,
		Thumbnails: thumbnails(list(r, "thumbnail", "thumbnails")),
		Timestamp:  x.timestamp(timeText),
		LiveStatus: live,
	}
	if upcoming {
		e.ReleaseTimestamp = int64(starts)
	}
	e.Uploader = firstText(r, "ownerText", "shortBylineText")
	if e.Uploader == "" {
		e.Uploader = text(get(reel, "channelTitleText"))
	}
	// Live and upcoming streams give how many are watching or waiting, not views.
	if live != "is_live" && live != "is_upcoming" {
		views := firstText(r, "viewCountText", "shortViewCountText", "videoInfo")
		var n int64
		var known bool
		if strings.Contains(strings.ToLower(views), "no views") {
			n, known = 0, true
		} else {
			n, known = getCount(views)
		}
		if known {
			e.ViewCount = &n
		}
	}
	switch {
	case badges[badgePublic]:
		e.Availability = "public"
	default:
		e.Availability = availability(badges)
	}
	return e
}

// channel reads a channelRenderer (_extract_channel_renderer).
func (x extractor) channel(r map[string]any) entry {
	id := str(r, "channelId")
	if !ucidRe.MatchString(id) {
		id = ""
	}
	link := ""
	if id != "" {
		link = "https://www.youtube.com/channel/" + id
	}
	title := text(r["title"])
	return entry{Type: "url", IEKey: "YoutubeTab", ID: id, URL: link, Title: title, Uploader: title,
		Thumbnails: thumbnails(list(r, "thumbnail", "thumbnails"))}
}

var ucidRe = regexp.MustCompile(`^UC[\w-]{22}$`)

// lockup reads a lockupViewModel, which is how playlists and mixes come (_extract_lockup_view_model).
func (x extractor) lockup(vm map[string]any) (entry, bool) {
	id := str(vm, "contentId")
	if id == "" {
		return entry{}, false
	}
	e := entry{Type: "url", ID: id}
	var thumbPath []any
	switch str(vm, "contentType") {
	case "LOCKUP_CONTENT_TYPE_VIDEO":
		e.IEKey, e.URL = "Youtube", "https://www.youtube.com/watch?v="+id
		thumbPath = []any{"contentImage", "thumbnailViewModel", "image", "sources"}
	case "LOCKUP_CONTENT_TYPE_PLAYLIST", "LOCKUP_CONTENT_TYPE_PODCAST":
		e.IEKey, e.URL = "YoutubeTab", "https://www.youtube.com/playlist?list="+id
		thumbPath = []any{"contentImage", "collectionThumbnailViewModel", "primaryThumbnail", "thumbnailViewModel", "image", "sources"}
	default:
		return entry{}, false
	}
	meta := obj(vm, "metadata", "lockupMetadataViewModel")
	content := obj(meta, "metadata", "contentMetadataViewModel")
	e.Title = str(meta, "title", "content")
	e.Thumbnails = thumbnails(list(vm, thumbPath...))

	var badgeTexts, badgeStyles []string
	for _, o := range list(vm, "contentImage", "thumbnailViewModel", "overlays") {
		var badges []any
		badges = append(badges, list(o, "thumbnailBottomOverlayViewModel", "badges")...)
		badges = append(badges, list(o, "thumbnailOverlayBadgeViewModel", "thumbnailBadges")...)
		for _, b := range badges {
			tb := obj(b, "thumbnailBadgeViewModel")
			if tb == nil {
				continue
			}
			if t, ok := tb["text"].(string); ok {
				badgeTexts = append(badgeTexts, strings.ToLower(t))
			}
			if s, ok := tb["badgeStyle"].(string); ok {
				badgeStyles = append(badgeStyles, s)
			}
		}
	}
	durationText := ""
	if len(badgeTexts) > 0 {
		durationText = badgeTexts[0]
	}
	e.Duration, _ = parseDuration(durationText)

	rows := list(content, "metadataRows")
	// The channel is the first part whose link goes to a channel.
	for _, row := range rows {
		for _, part := range list(row, "metadataParts") {
			if str(part, "text", "commandRuns", 0, "onTap", "innertubeCommand", "browseEndpoint", "browseId") != "" {
				e.Uploader = str(part, "text", "content")
				break
			}
		}
		if e.Uploader != "" {
			break
		}
	}
	if e.Uploader == "" {
		// Videos credited to several channels list them in a dialog.
		items := list(meta, "image", "avatarStackViewModel", "rendererContext", "commandContext", "onTap",
			"innertubeCommand", "showDialogCommand", "panelLoadingStrategy", "inlineContent", "dialogViewModel",
			"customContent", "listViewModel", "listItems")
		if len(items) > 0 {
			e.Uploader = str(items[0], "listItemViewModel", "title", "content")
		}
	}

	// The row whose last part has an accessibility label holds the views and the date.
	var viewsAndTime []any
	for _, row := range rows {
		parts := list(row, "metadataParts")
		if len(parts) == 0 {
			continue
		}
		if last, ok := parts[len(parts)-1].(map[string]any); ok {
			if _, has := last["accessibilityLabel"]; has {
				viewsAndTime = append(viewsAndTime, parts...)
			}
		}
	}
	relative := ""
	if n := len(viewsAndTime); n > 0 {
		relative = strings.ToLower(str(viewsAndTime[n-1], "text", "content"))
	}
	if len(viewsAndTime) == 2 {
		if n, ok := parseCount(str(viewsAndTime[0], "text", "content")); ok {
			e.ViewCount = &n
		}
	}
	e.Timestamp = x.timestamp(relative)
	switch {
	case durationText == "upcoming":
		e.LiveStatus = "is_upcoming"
	case slices.Contains(badgeStyles, "THUMBNAIL_OVERLAY_BADGE_STYLE_LIVE"):
		e.LiveStatus = "is_live"
	case relative != "" && strings.Contains(relative, "streamed"):
		e.LiveStatus = "was_live"
	}
	for _, row := range rows {
		for _, b := range list(row, "badges") {
			if str(b, "badgeViewModel", "badgeStyle") == "BADGE_MEMBERS_ONLY" {
				e.Availability = "subscriber_only"
			}
		}
	}
	return e, true
}

// ---- badges ----

type badge int

const (
	badgeUnlisted badge = iota
	badgePrivate
	badgePublic
	badgeVerified
	badgeSubscription
	badgePremium
	badgeLiveNow
)

var (
	badgeIcons = map[string]badge{
		"PRIVACY_UNLISTED": badgeUnlisted, "PRIVACY_PRIVATE": badgePrivate, "PRIVACY_PUBLIC": badgePublic,
		"CHECK_CIRCLE_THICK": badgeVerified, "OFFICIAL_ARTIST_BADGE": badgeVerified, "CHECK": badgeVerified,
	}
	badgeStylesMap = map[string]badge{
		"BADGE_STYLE_TYPE_MEMBERS_ONLY": badgeSubscription, "BADGE_STYLE_TYPE_PREMIUM": badgePremium,
		"BADGE_STYLE_TYPE_LIVE_NOW": badgeLiveNow, "BADGE_STYLE_TYPE_VERIFIED": badgeVerified,
		"BADGE_STYLE_TYPE_VERIFIED_ARTIST": badgeVerified,
	}
	// Tried in this order, as yt-dlp's dict is.
	badgeLabels = []struct {
		match string
		b     badge
	}{
		{"unlisted", badgeUnlisted}, {"private", badgePrivate}, {"members only", badgeSubscription},
		{"live", badgeLiveNow}, {"premium", badgePremium}, {"verified", badgeVerified},
		{"official artist channel", badgeVerified},
	}
)

var badgeKeyRe = regexp.MustCompile(`[bB]adgeRenderer$`)

// extractBadges reads a renderer's badges (_extract_badges).
func extractBadges(list []any) map[badge]bool {
	out := map[badge]bool{}
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for key, v := range m {
			if !badgeKeyRe.MatchString(key) {
				continue
			}
			b, _ := v.(map[string]any)
			if t, ok := badgeIcons[str(b, "icon", "iconType")]; ok {
				out[t] = true
				continue
			}
			if t, ok := badgeStylesMap[str(b, "style")]; ok {
				out[t] = true
				continue
			}
			label := ""
			for _, p := range [][]any{{"label"}, {"accessibilityData", "label"}, {"tooltip"}, {"iconTooltip"}} {
				if label = str(b, p...); label != "" {
					break
				}
			}
			for _, l := range badgeLabels {
				if strings.Contains(strings.ToLower(label), l.match) {
					out[l.b] = true
					break
				}
			}
		}
	}
	return out
}

// availability is yt-dlp's _availability for the badges a video shows.
func availability(b map[badge]bool) string {
	switch {
	case b[badgePrivate]:
		return "private"
	case b[badgePremium]:
		return "premium_only"
	case b[badgeSubscription]:
		return "subscriber_only"
	case b[badgeUnlisted]:
		return "unlisted"
	}
	return ""
}

// ---- values ----

// thumbnails reads a list of {url, width, height} (_extract_thumbnails).
func thumbnails(items []any) []thumb {
	var out []thumb
	for _, it := range items {
		u := str(it, "url")
		if !thumbURLRe.MatchString(u) {
			continue
		}
		// YouTube sometimes gives maxresdefault links with a query that doesn't work.
		if strings.Contains(u, "maxresdefault") {
			u, _, _ = strings.Cut(u, "?")
		}
		w, _ := toInt(get(it, "width"))
		h, _ := toInt(get(it, "height"))
		out = append(out, thumb{URL: u, Width: int(w), Height: int(h)})
	}
	return out
}

var thumbURLRe = regexp.MustCompile(`^(?:(?:https?|rt(?:m(?:pt?[es]?|fp)|sp[su]?)|mms|ftps?|wss?):)?//`)

// relativeRe is extract_relative_time's pattern; longer units come first, as there.
var relativeRe = regexp.MustCompile(`(today|yesterday|now)|(\d+)\s*(second|minute|month|hour|week|year|sec|min|day|wk|hr|mo|yr|s|h|d|w|y)s?\s*ago`)

var relativeUnits = map[string]string{
	"s": "second", "sec": "second", "second": "second", "min": "minute", "minute": "minute",
	"h": "hour", "hr": "hour", "hour": "hour", "d": "day", "day": "day", "w": "week", "wk": "week", "week": "week",
	"mo": "month", "month": "month", "y": "year", "yr": "year", "year": "year",
}

var absoluteDateRe = regexp.MustCompile(`([a-z]+\s*\d{1,2},?\s*20\d{2})`)

// parseTimeText turns "3 years ago" or "Streamed 2 days ago" into a Unix time, rounded like
// yt-dlp's datetime_from_str (to the day for days and longer); 0 when it can't.
func parseTimeText(s string, now time.Time) int64 {
	if s == "" {
		return 0
	}
	now = now.UTC()
	if m := relativeRe.FindStringSubmatch(s); m != nil {
		switch m[1] {
		case "today", "now":
			return now.Unix()
		case "yesterday":
			return now.AddDate(0, 0, -1).Unix()
		}
		n, err := strconv.Atoi(m[2])
		if err != nil {
			return 0
		}
		var t time.Time
		round := int64(86400)
		switch relativeUnits[m[3]] {
		case "year":
			t = addMonths(now, -12*n)
		case "month":
			t = addMonths(now, -n)
		case "week":
			t = now.AddDate(0, 0, -7*n)
		case "day":
			t = now.AddDate(0, 0, -n)
		case "hour":
			t, round = now.Add(-time.Duration(n)*time.Hour), 3600
		case "minute":
			t, round = now.Add(-time.Duration(n)*time.Minute), 60
		case "second":
			t, round = now.Add(-time.Duration(n)*time.Second), 1
		}
		return roundTo(t, round)
	}
	if m := absoluteDateRe.FindStringSubmatch(strings.ToLower(s)); m != nil {
		for _, layout := range []string{"Jan 2, 2006", "Jan 2 2006", "January 2, 2006", "January 2 2006"} {
			if t, err := time.Parse(layout, titleCase(m[1])); err == nil {
				return t.Unix()
			}
		}
	}
	return 0
}

// addMonths is yt-dlp's datetime_add_months: the day is kept unless the month is shorter.
func addMonths(t time.Time, months int) time.Time {
	y, m, d := t.Date()
	total := int(m) - 1 + months
	y += total / 12
	if total%12 < 0 {
		y--
	}
	mon := time.Month((total%12+12)%12 + 1)
	last := time.Date(y, mon+1, 0, 0, 0, 0, 0, time.UTC).Day()
	return time.Date(y, mon, min(d, last), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
}

// roundTo rounds t to a multiple of n seconds, halves up, as datetime_round does.
func roundTo(t time.Time, n int64) int64 {
	x := float64(t.UnixNano()) / 1e9
	return int64(math.Floor((x+float64(n)/2)/float64(n))) * n
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

var (
	colonDurationRe = regexp.MustCompile(`^(?:(?:(?:(\d+):)?(\d+):)?(\d+):)?(\d+)([.:]\d+)?Z?$`)
	textDurationRe  = regexp.MustCompile(`(?i)^(?:P?(?:\d+\s*y(?:ears?)?,?\s*)?(?:\d+\s*m(?:onths?)?,?\s*)?(?:\d+\s*w(?:eeks?)?,?\s*)?(?:(\d+)\s*d(?:ays?)?,?\s*)?T)?(?:(\d+)\s*h(?:(?:ou)?rs?)?,?\s*)?(?:(\d+)\s*m(?:in(?:ute)?s?)?,?\s*)?(?:(\d+)(\.\d+)?\s*s(?:ec(?:ond)?s?)?\s*)?Z?$`)
	looseDurationRe = regexp.MustCompile(`(?i)^(?:([0-9.]+)\s*(?:hours?)|([0-9.]+)\s*(?:mins?\.?|minutes?)\s*)Z?$`)
)

// parseDuration is yt-dlp's parse_duration for the forms YouTube uses: "1:02:03", "3:45",
// "1 hour, 2 minutes".
func parseDuration(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	var days, hours, mins, secs, ms string
	if m := colonDurationRe.FindStringSubmatch(s); m != nil && (m[3] == "" || len(m[4]) <= 2) {
		days, hours, mins, secs, ms = m[1], m[2], m[3], m[4], m[5]
	} else if m := textDurationRe.FindStringSubmatch(s); m != nil {
		days, hours, mins, secs, ms = m[1], m[2], m[3], m[4], m[5]
	} else if m := looseDurationRe.FindStringSubmatch(s); m != nil {
		hours, mins = m[1], m[2]
	} else {
		return 0, false
	}
	ms = strings.Replace(ms, ":", ".", 1)
	total := 0.0
	for _, p := range []struct {
		v    string
		mult float64
	}{{days, 86400}, {hours, 3600}, {mins, 60}, {secs, 1}, {ms, 1}} {
		if p.v != "" {
			f, err := strconv.ParseFloat(p.v, 64)
			if err != nil {
				return 0, false
			}
			total += f * p.mult
		}
	}
	return total, true
}

// durationFromLabel finds the length in a Short's accessibility label, after its last "ago":
// "... 3 days ago 1 minute, 5 seconds 1,234 views - play Short".
func durationFromLabel(label string) (float64, bool) {
	i := strings.LastIndex(strings.ToLower(label), "ago")
	if i < 0 {
		return 0, false
	}
	m := labelDurationRe.FindStringSubmatch(label[i+3:])
	if m == nil {
		return 0, false
	}
	return parseDuration(m[1])
}

var labelDurationRe = regexp.MustCompile(`(?i)^\s+([a-z0-9 ,]+?)(?:\s+[\d,]+\s+views)?(?:\s+-\s+play\s+short)?$`)

// getCount reads "1,234 views" or "1.2M views" (_get_count).
func getCount(s string) (int64, bool) {
	if strings.HasPrefix(strings.ToLower(s), "no ") {
		return 0, true
	}
	if n, ok := parseCount(s); ok {
		return n, true
	}
	if m := leadingDigitsRe.FindStringSubmatch(strings.Join(strings.Fields(s), "")); m != nil {
		return digits(m[1])
	}
	return 0, false
}

var (
	leadingDigitsRe = regexp.MustCompile(`^([\d,]+)`)
	countPrefixRe   = regexp.MustCompile(`^[^\d]+\s`)
	plainCountRe    = regexp.MustCompile(`^[\d,.]+$`)
	unitCountRe     = regexp.MustCompile(`^([\d,.]+)(?:\s*)(kk|KK|k|K|m|M|b|B)\b`)
)

// parseCount is yt-dlp's parse_count: "1,234", "1.2M views", "3K".
func parseCount(s string) (int64, bool) {
	s = strings.TrimSpace(countPrefixRe.ReplaceAllString(s, ""))
	if s == "" {
		return 0, false
	}
	if plainCountRe.MatchString(s) {
		return digits(s)
	}
	if m := unitCountRe.FindStringSubmatch(s); m != nil {
		f, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
		if err != nil {
			return 0, false
		}
		mult := map[string]float64{"k": 1e3, "K": 1e3, "kk": 1e6, "KK": 1e6, "m": 1e6, "M": 1e6, "b": 1e9, "B": 1e9}[m[2]]
		return int64(f * mult), true
	}
	return 0, false
}

// digits is str_to_int: the number with its separators dropped.
func digits(s string) (int64, bool) {
	s = strings.NewReplacer(",", "", ".", "", "+", "").Replace(s)
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil
}

// ---- JSON access ----

// get follows a path of object keys and list indices; nil when anything is missing.
func get(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, ok := v.(map[string]any)
			if !ok {
				return nil
			}
			v = m[k]
		case int:
			l, ok := v.([]any)
			if !ok || k >= len(l) {
				return nil
			}
			v = l[k]
		}
	}
	return v
}

func obj(v any, path ...any) map[string]any {
	m, _ := get(v, path...).(map[string]any)
	return m
}

func list(v any, path ...any) []any {
	l, _ := get(v, path...).([]any)
	return l
}

func str(v any, path ...any) string {
	s, _ := get(v, path...).(string)
	return s
}

// toInt reads a number given as a number or a string.
func toInt(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case string:
		return digits(n)
	}
	return 0, false
}

// text reads YouTube's text objects: {"simpleText": "..."} or {"runs": [{"text": "..."}, ...]}.
func text(v any) string {
	if s := str(v, "simpleText"); s != "" {
		return s
	}
	runs := list(v, "runs")
	if runs == nil {
		runs, _ = v.([]any)
	}
	var b strings.Builder
	for _, r := range runs {
		b.WriteString(str(r, "text"))
	}
	return b.String()
}

// firstText is the first of a renderer's text fields that says something.
func firstText(r map[string]any, keys ...string) string {
	for _, k := range keys {
		if t := text(r[k]); t != "" {
			return t
		}
	}
	return ""
}

func absURL(path string) string {
	if path == "" {
		return ""
	}
	base, _ := url.Parse("https://www.youtube.com/")
	u, err := base.Parse(path)
	if err != nil {
		return ""
	}
	return u.String()
}
