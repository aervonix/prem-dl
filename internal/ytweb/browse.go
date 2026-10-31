package ytweb

// Channel tabs and playlists, the way yt-dlp's YouTube tab extractor lists them with
// --flat-playlist (_tab.py: YoutubeTabIE._real_extract, _extract_from_tabs, _entries).
//
// yt-dlp reads a channel tab's web page for the data embedded in it (ytInitialData), then
// follows continuation tokens through the browse API. For a playlist it reads the page too, then
// throws that away for the browse API's answer with unavailable videos shown, which is all this
// asks for. A channel's home page is its Videos tab with its Live and Shorts tabs, one after
// another in yt-dlp and side by side here. yt-dlp took 0.7-2.5 s for these; this ~0.25-0.6 s.

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"yt-dlp-gui/internal/ytdlp"
)

const (
	browseAPI = "/youtubei/v1/browse?prettyPrint=false"
	// maxContinuations bounds one tab's pages (30 videos each): 300,000 videos is past any
	// channel, and a loop of fresh tokens can't run for ever.
	maxContinuations = 10000
)

// errNotHere is for links and pages yt-dlp handles: odd links, missing tabs, alerts, redirects.
// Its message names what it saw, for the log; callers list the link with yt-dlp instead.
type errNotHere string

func (e errNotHere) Error() string { return "left to yt-dlp: " + string(e) }

// Lister lists channel tabs and playlists.
type Lister struct {
	base string           // https://www.youtube.com; replaced in tests
	now  func() time.Time // dates "3 years ago"
}

// NewLister returns a Lister.
func NewLister() *Lister { return NewListerAt("https://www.youtube.com") }

// NewListerAt returns a Lister that reads base instead of https://www.youtube.com, for tests.
func NewListerAt(base string) *Lister { return &Lister{base: base, now: time.Now} }

// target is a link List reads itself.
type target struct {
	id   string // what yt-dlp calls it: "@name", a channel's or playlist's ID, a /c/ name
	path string // the channel's home page, "/@name"; "" for a playlist
	tab  string // "" for a channel's home page, or videos, shorts, streams, playlists
	list string // the playlist's ID
	url  string // the link as yt-dlp gives it back (webpage_url)
}

var (
	listedTabs = map[string]bool{"videos": true, "shorts": true, "streams": true, "playlists": true}
	idRe       = regexp.MustCompile(`^[\w-]+$`)
)

// Handles reports whether List reads link itself. Everything else, and anything List fails
// at, is for yt-dlp.
func Handles(link string) bool {
	_, ok := parseTarget(link)
	return ok
}

func parseTarget(link string) (target, bool) {
	norm, err := ytdlp.NormalizeURL(link)
	if err != nil {
		return target{}, false
	}
	u, err := url.Parse(norm)
	if err != nil || u.Fragment != "" {
		return target{}, false
	}
	host := strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(u.Hostname()), "www."), "m.")
	if host != "youtube.com" {
		return target{}, false
	}
	if u.Path == "/playlist" {
		// Mixes (RD), Watch later, liked and saved music need yt-dlp: they're read another way,
		// or only signed in.
		id := u.Query().Get("list")
		switch {
		case !idRe.MatchString(id), !hasAnyPrefix(id, "PL", "UU", "OL", "FL"):
			return target{}, false
		}
		return target{id: id, list: id, url: "https://www.youtube.com/playlist?" + u.RawQuery}, true
	}
	if u.RawQuery != "" {
		return target{}, false
	}
	home, tab, ok := ytdlp.YoutubeChannel(norm)
	if !ok || (tab != "" && !listedTabs[tab]) {
		return target{}, false
	}
	segs := strings.FieldsFunc(u.Path, func(r rune) bool { return r == '/' })
	n := 1
	if !strings.HasPrefix(segs[0], "@") {
		n = 2
	}
	if len(segs) > n+1 || (tab != "" && segs[n] != tab) {
		return target{}, false // more after the tab, or a tab in capitals
	}
	t := target{id: segs[n-1], path: strings.TrimPrefix(home, "https://www.youtube.com"), tab: tab, url: home}
	if tab != "" {
		t.url = home + "/" + tab
	}
	return t, true
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

// Progress is told about a listing while it is read, in parts: the first holds the listing's
// title and the like with its first page of entries (perhaps none), each later one the entries
// that follow. Entries come in their final order, numbered from 1 across the parts; a channel's
// Live and Shorts tabs come after its last video, as they do in the whole listing.
type Progress func(part *ytdlp.ProbeResult)

// List reads a channel tab, a channel's home page or a playlist: at most limit entries of each
// tab (0 for all), as yt-dlp's --playlist-end does. Links Handles refuses, and pages laid out in
// a way yt-dlp would handle differently, fail with an error; callers then ask yt-dlp. progress,
// if not nil, gets the listing in parts as it is read; a channel takes a request (~0.25 s) per
// 30 videos.
func (l *Lister) List(ctx context.Context, o Options, link string, limit int, progress Progress) (*ytdlp.ProbeResult, error) {
	t, ok := parseTarget(link)
	if !ok {
		return nil, errNotHere("not a channel tab or playlist link")
	}
	hc := pool.get(o)
	out := &parts{progress: progress}
	var pl map[string]any
	var err error
	switch {
	case t.list != "":
		pl, err = l.playlist(ctx, hc, t, limit, out.flat)
	case t.tab != "":
		var tp tabPage
		if tp, err = l.page(ctx, hc, t.path+"/"+t.tab); err == nil {
			pl, err = l.tab(ctx, hc, tp, t, t.tab, limit, out.flat)
		}
	default:
		pl, err = l.home(ctx, hc, t, limit, out)
	}
	if err == nil {
		err = out.err
	}
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(pl)
	if err != nil {
		return nil, err
	}
	return ytdlp.ParseProbe(data)
}

// tabPage is a channel tab's page: its embedded data and the client settings it gave.
type tabPage struct {
	data    map[string]any
	version string // INNERTUBE_CLIENT_VERSION
	visitor string // VISITOR_DATA
}

var (
	initialDataRe = regexp.MustCompile(`(?:window\s*\[\s*["']ytInitialData["']\s*\]|ytInitialData)\s*=\s*`)
	versionRe     = regexp.MustCompile(`"INNERTUBE_CLIENT_VERSION"\s*:\s*"([^"]+)"`)
	visitorRe     = regexp.MustCompile(`"VISITOR_DATA"\s*:\s*"([^"]+)"`)
)

func (l *Lister) page(ctx context.Context, hc *http.Client, path string) (tabPage, error) {
	html, err := getPage(ctx, hc, l.base+path)
	if err != nil {
		return tabPage{}, err
	}
	return readPage(html)
}

// readPage finds the data a channel page embeds (extract_yt_initial_data, extract_ytcfg).
func readPage(html []byte) (tabPage, error) {
	var p tabPage
	for _, loc := range initialDataRe.FindAllIndex(html, -1) {
		dec := json.NewDecoder(strings.NewReader(string(html[loc[1]:])))
		if dec.Decode(&p.data) == nil && p.data != nil {
			break
		}
		p.data = nil
	}
	if p.data == nil {
		return tabPage{}, errLayout
	}
	if m := versionRe.FindSubmatch(html); m != nil {
		p.version = string(m[1])
	}
	if m := visitorRe.FindSubmatch(html); m != nil {
		p.visitor = string(m[1])
	}
	return p, nil
}

// check refuses pages yt-dlp would stop at or treat specially: errors YouTube shows ("This
// channel does not exist"), a redirect to a regional channel, data missing.
func check(data map[string]any) error {
	for _, a := range list(data, "alerts") {
		m, _ := a.(map[string]any)
		for _, v := range m {
			if strings.EqualFold(str(v, "type"), "error") {
				return errNotHere("YouTube says: " + firstText(v.(map[string]any), "text"))
			}
		}
	}
	for _, a := range list(data, "onResponseReceivedActions") {
		if str(a, "navigateAction", "endpoint", "commandMetadata", "webCommandMetadata", "url") != "" {
			return errNotHere("a redirect to another channel")
		}
	}
	if _, ok := data["contents"]; !ok {
		return errNotHere("a page without contents")
	}
	return nil
}

// tabRenderers lists a page's tabs (_extract_tab_renderers).
func tabRenderers(data map[string]any) []map[string]any {
	var out []map[string]any
	for _, t := range list(data, "contents", "twoColumnBrowseResultsRenderer", "tabs") {
		if r := obj(t, "tabRenderer"); r != nil {
			out = append(out, r)
		} else if r := obj(t, "expandableTabRenderer"); r != nil {
			out = append(out, r)
		}
	}
	return out
}

func selectedTab(tabs []map[string]any) map[string]any {
	for _, t := range tabs {
		if b, _ := t["selected"].(bool); b {
			return t
		}
	}
	return nil
}

// tabID names a tab the way its link does: videos, shorts, streams... (_extract_tab_id_and_name).
func tabID(tab map[string]any) string {
	if link := str(tab, "endpoint", "commandMetadata", "webCommandMetadata", "url"); link != "" {
		segs := strings.FieldsFunc(strings.SplitN(link, "?", 2)[0], func(r rune) bool { return r == '/' })
		n := 1
		if len(segs) > 0 && !strings.HasPrefix(segs[0], "@") {
			n = 2
		}
		if len(segs) > n {
			return segs[n]
		}
	}
	if id := str(tab, "tabIdentifier"); id != "" {
		if id == "TAB_ID_SPONSORSHIPS" {
			return "membership"
		}
		return id
	}
	switch name := strings.ToLower(str(tab, "title")); name {
	case "home":
		return "featured"
	case "live":
		return "streams"
	default:
		return name
	}
}

func hasTab(tabs []map[string]any, id string) bool {
	for _, t := range tabs {
		if tabID(t) == id {
			return true
		}
	}
	return false
}

// metadata is what a listing says about the channel or playlist (_extract_metadata_from_tabs).
type metadata struct {
	id, title, channel, channelID string
}

var ownersRe = regexp.MustCompile(`^by (.+) and \d+ others?$`)

func metadataFrom(itemID string, data map[string]any) metadata {
	m := metadata{id: itemID}
	mr := obj(data, "metadata", "channelMetadataRenderer")
	if mr != nil {
		m.channel = str(mr, "title")
		m.channelID = ucid(str(mr, "externalId"))
		if m.channelID == "" {
			m.channelID = ucidFromURL(str(mr, "channelUrl"))
		}
		if m.channelID != "" {
			m.id = m.channelID
		}
	} else {
		mr = obj(data, "metadata", "playlistMetadataRenderer")
	}
	m.title = str(mr, "title")
	if m.title == "" {
		m.title = text(get(data, "header", "hashtagHeaderRenderer", "hashtag"))
	}
	if m.title == "" {
		m.title = m.id
	}
	if m.channelID == "" {
		owner := get(data, "header", "playlistHeaderRenderer", "ownerText")
		if owner == nil {
			for _, it := range list(data, "sidebar", "playlistSidebarRenderer", "items") {
				if o := get(it, "playlistSidebarSecondaryInfoRenderer", "videoOwner", "videoOwnerRenderer", "title"); o != nil {
					owner = o
					break
				}
			}
		}
		m.channel = text(owner)
		if g := ownersRe.FindStringSubmatch(m.channel); g != nil {
			m.channel = g[1]
		}
		m.channelID = ucid(str(owner, "runs", 0, "navigationEndpoint", "browseEndpoint", "browseId"))
	}
	return m
}

func ucid(s string) string {
	if ucidRe.MatchString(s) {
		return s
	}
	return ""
}

var ucidURLRe = regexp.MustCompile(`/channel/(UC[\w-]{22})`)

func ucidFromURL(s string) string {
	if m := ucidURLRe.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

// parts hands a listing to a Progress as it is read.
type parts struct {
	progress Progress
	top      map[string]any // the listing without its entries; nil until the first page
	nested   bool           // a home page: entries go in tabs, named by their title
	started  bool           // the first part went
	sent     int            // entries sent
	err      error
}

// flat sends a page of a single tab or playlist; header is the listing without its entries.
func (p *parts) flat(header map[string]any, es []entry) {
	if p.top == nil {
		p.top = header
	}
	p.send("", es)
}

// send hands on entries, in the tab titled group when nested. The first part goes even empty:
// it carries the title.
func (p *parts) send(group string, es []entry) {
	if p.progress == nil || p.err != nil || (p.started && len(es) == 0) {
		return
	}
	wrap := maps.Clone(p.top)
	if es == nil {
		es = []entry{}
	}
	wrap["entries"] = es
	if p.nested {
		wrap["entries"] = []any{map[string]any{"_type": "playlist", "title": group, "entries": es}}
	}
	data, err := json.Marshal(wrap)
	if err != nil {
		p.err = err
		return
	}
	res, err := ytdlp.ParseProbe(data)
	if err != nil {
		p.err = err
		return
	}
	for i := range res.Playlist.Entries {
		res.Playlist.Entries[i].Index = p.sent + i + 1
	}
	p.sent += len(res.Playlist.Entries)
	p.started = true
	p.progress(res)
}

// onPage is told about each page of a tab as it is read: the tab without its entries, and the
// page's entries (already cut to the limit).
type onPage func(header map[string]any, es []entry)

// playlistJSON is a listing in yt-dlp's -J shape.
func playlistJSON(m metadata, title, webpage string, entries any) map[string]any {
	return map[string]any{
		"_type": "playlist", "id": m.id, "title": title, "uploader": m.channel, "channel": m.channel,
		"channel_id": m.channelID, "webpage_url": webpage, "entries": entries,
	}
}

// tab lists the tab a page shows, which must be the one asked for (_extract_from_tabs).
func (l *Lister) tab(ctx context.Context, hc *http.Client, p tabPage, t target, want string, limit int, report onPage) (map[string]any, error) {
	if err := check(p.data); err != nil {
		return nil, err
	}
	tabs := tabRenderers(p.data)
	sel := selectedTab(tabs)
	switch {
	case sel == nil:
		return nil, errNotHere("no tab selected")
	case str(sel, "tabIdentifier") == "FEwhat_to_watch":
		return nil, errNotHere("YouTube's home page instead of the channel")
	case tabID(sel) != want:
		return nil, errNotHere("the channel has no " + want + " tab")
	}
	m := metadataFrom(t.id, p.data)
	title := m.title
	for _, k := range []string{"title", "expandedText"} {
		if s := str(sel, k); s != "" {
			title += " - " + s
		}
	}
	webpage := t.url
	if t.list == "" {
		webpage = "https://www.youtube.com" + t.path + "/" + want
	}
	header := playlistJSON(m, title, webpage, nil)
	page := func(es []entry) {
		if report != nil {
			report(header, es)
		}
	}
	x := extractor{now: l.now(), dates: true}
	var entries []entry
	content := obj(sel, "content")
	parent := obj(content, "sectionListRenderer")
	if parent == nil {
		parent = obj(content, "richGridRenderer")
	}
	next := x.entries(parent, &entries)
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	page(entries)
	visitor := str(p.data, "responseContext", "visitorData")
	if visitor == "" {
		visitor = p.visitor
	}
	entries, err := l.follow(ctx, hc, x, entries, next, limit, p.version, visitor, page)
	if err != nil {
		return nil, err
	}
	header["entries"] = entries
	return header, nil
}

// follow follows continuation tokens until limit entries are read (0: all), or the tokens
// run out or come round again (_entries).
func (l *Lister) follow(ctx context.Context, hc *http.Client, x extractor, entries []entry, next string, limit int, version, visitor string, page func([]entry)) ([]entry, error) {
	seen := map[string]bool{}
	for pages := 0; next != "" && (limit == 0 || len(entries) < limit) && !seen[next] && pages < maxContinuations; pages++ {
		seen[next] = true
		raw, err := callAPI(ctx, hc, l.base+browseAPI, map[string]any{"continuation": next}, version, visitor)
		if err != nil {
			return nil, err
		}
		var root map[string]any
		if err := json.Unmarshal(raw, &root); err != nil {
			return nil, err
		}
		p := parseContinuation(root, x)
		got := p.entries
		if limit > 0 {
			got = got[:min(len(got), limit-len(entries))]
		}
		entries = append(entries, got...)
		page(got)
		next = p.next
		if p.visitor != "" {
			visitor = p.visitor
		}
	}
	if entries == nil {
		entries = []entry{}
	}
	return entries, nil
}

// playlist lists a playlist, as the browse API answers yt-dlp's reload of it with unavailable
// videos shown (_reload_with_unavailable_videos).
func (l *Lister) playlist(ctx context.Context, hc *http.Client, t target, limit int, report onPage) (map[string]any, error) {
	raw, err := callAPI(ctx, hc, l.base+browseAPI, map[string]any{"browseId": "VL" + t.list, "params": "wgYCCAA="}, "", "")
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	if obj(data, "metadata", "playlistMetadataRenderer") == nil && obj(data, "header", "playlistHeaderRenderer") == nil {
		return nil, errNotHere("not laid out as a playlist")
	}
	return l.tab(ctx, hc, tabPage{data: data}, t, tabID(selectedTab(tabRenderers(data))), limit, report)
}

// home lists a channel's home page: its Videos tab and, as yt-dlp does, its Live and Shorts
// tabs, each a nested list. A channel with only videos is its Videos tab alone. The other tabs
// are read alongside the Videos tab; their pages are handed on after it, in order, each as soon
// as the tabs before it are done and it has been read.
func (l *Lister) home(ctx context.Context, hc *http.Client, t target, limit int, out *parts) (map[string]any, error) {
	videos, err := l.page(ctx, hc, t.path+"/videos")
	if err != nil {
		return nil, err
	}
	if err := check(videos.data); err != nil {
		return nil, err
	}
	tabs := tabRenderers(videos.data)
	if sel := selectedTab(tabs); sel == nil || tabID(sel) != "videos" {
		return nil, errNotHere("a channel without a videos tab")
	}
	var extra []string
	for _, id := range []string{"streams", "shorts"} {
		if hasTab(tabs, id) {
			extra = append(extra, id)
		}
	}
	m := metadataFrom(t.id, videos.data)
	top := playlistJSON(m, m.title, t.url, nil)
	top["id"] = t.id // the link's own ID, not the channel's (playlist_result's playlist_id)
	report := out.flat
	if len(extra) > 0 {
		out.top, out.nested = top, true
		report = func(header map[string]any, es []entry) { out.send(header["title"].(string), es) }
	}

	// The other tabs are read while the Videos tab's further pages are, and stop if it fails.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make([]map[string]any, 1+len(extra))
	errs := make([]error, len(results))
	queues := make([]*pageQueue, len(extra))
	var wg sync.WaitGroup
	for i, id := range extra {
		q := newPageQueue()
		queues[i] = q
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer q.close()
			p, err := l.page(ctx, hc, t.path+"/"+id)
			if err == nil {
				results[i+1], err = l.tab(ctx, hc, p, t, id, limit, q.push)
			}
			errs[i+1] = err
		}()
	}
	results[0], errs[0] = l.tab(ctx, hc, videos, t, "videos", limit, report)
	if errs[0] == nil {
		for _, q := range queues {
			q.drain(out.send)
		}
	}
	cancel()
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	if len(extra) == 0 {
		return results[0], nil
	}
	top["entries"] = results
	return top, nil
}

// pageQueue keeps a tab's pages, read alongside the tabs before it, until they can be handed on.
type pageQueue struct {
	mu    sync.Mutex
	pages [][]entry
	title string
	done  bool
	wake  chan struct{} // closed, and replaced, when a page comes or the tab is done
}

func newPageQueue() *pageQueue { return &pageQueue{wake: make(chan struct{})} }

// push is the tab's onPage.
func (q *pageQueue) push(header map[string]any, es []entry) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.title, _ = header["title"].(string)
	q.pages = append(q.pages, es)
	q.signal()
}

// close says the tab is done (or failed).
func (q *pageQueue) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.done = true
	q.signal()
}

func (q *pageQueue) signal() {
	close(q.wake)
	q.wake = make(chan struct{})
}

// drain hands on the tab's pages as they come, until it is done.
func (q *pageQueue) drain(send func(group string, es []entry)) {
	for {
		q.mu.Lock()
		pages, title, done, wake := q.pages, q.title, q.done, q.wake
		q.pages = nil
		q.mu.Unlock()
		for _, es := range pages {
			send(title, es)
		}
		if done {
			return
		}
		<-wake
	}
}
