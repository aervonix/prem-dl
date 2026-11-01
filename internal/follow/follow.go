// Package follow keeps the channels and playlists the user follows, checks them for new uploads
// every few hours, and queues what is new.
//
// Following a feed first notes every video already there (the baseline), so only uploads that
// appear after that are queued. Each later check lists the feed again and queues the videos it
// hasn't seen. YouTube channel tabs and TikTok profiles list the newest first, so a check reads
// only the top of them; playlists and other sites are read in full.
package follow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"yt-dlp-gui/internal/apperr"
	"yt-dlp-gui/internal/queue"
	"yt-dlp-gui/internal/settings"
	"yt-dlp-gui/internal/tiktok"
	"yt-dlp-gui/internal/ytdlp"
)

// Feed is a followed channel or playlist. It is sent to the frontend as JSON.
type Feed struct {
	ID       string `json:"id"`
	URL      string `json:"url"` // a YouTube channel's home page when Tabs is set
	Title    string `json:"title"`
	Uploader string `json:"uploader"`
	// Tabs are the YouTube channel tabs checked: videos, shorts, streams. Empty for anything
	// that isn't a YouTube channel, whose URL is checked as it is.
	Tabs    []string                 `json:"tabs"`
	Options settings.DownloadOptions `json:"options"`
	Filter  Filter                   `json:"filter"`
	// RecordLive queues a stream as soon as a check sees it scheduled or live, recorded from its
	// start, instead of waiting for it to end and taking the replay (which can be cut, edited or
	// made members-only).
	RecordLive bool `json:"recordLive"`
	Paused     bool `json:"paused"`

	AddedAt   time.Time `json:"addedAt" ts_type:"string"`
	LastCheck time.Time `json:"lastCheck" ts_type:"string"` // when the last check ended; zero before the first
	LastFound time.Time `json:"lastFound" ts_type:"string"` // when a check last queued something
	LastNew   int       `json:"lastNew"`                    // videos the last check queued
	Queued    int       `json:"queued"`                     // videos queued since following
	// Ready is set once the videos that were there when the feed was followed have been noted,
	// so they aren't taken for new uploads.
	Ready    bool             `json:"ready"`
	Checking bool             `json:"checking"`
	Error    *apperr.AppError `json:"error"` // why the last check failed; nil when it worked
}

// NewFeed is what the UI sends to follow something.
type NewFeed struct {
	URL      string                   `json:"url"`
	Title    string                   `json:"title"`
	Uploader string                   `json:"uploader"`
	Tabs     []string                 `json:"tabs"`
	Options  settings.DownloadOptions `json:"options"`
	Filter   Filter                   `json:"filter"`
	// RecordLive: see Feed.RecordLive.
	RecordLive bool `json:"recordLive"`
}

// Filter narrows what a check queues: a busy channel's long stream reruns, say, or only the
// episodes of a podcast. A video it turns away is noted like one already downloaded, so it isn't
// looked at again; changing the filter applies to uploads from then on.
type Filter struct {
	MinMinutes int `json:"minMinutes"` // shorter videos are skipped; 0 = no minimum
	MaxMinutes int `json:"maxMinutes"` // longer ones are skipped; 0 = no maximum
	// Include: the title must contain one of these (ignoring case); empty = any title.
	Include []string `json:"include"`
	// Exclude: titles containing any of these are skipped.
	Exclude []string `json:"exclude"`
}

const (
	maxFilterWords = 30
	maxFilterWord  = 100
	maxMinutes     = 100000
)

// clean trims the words and checks the numbers.
func (f *Filter) clean() error {
	if f.MinMinutes < 0 || f.MaxMinutes < 0 || f.MinMinutes > maxMinutes || f.MaxMinutes > maxMinutes {
		return errors.New("lengths are whole minutes, 0 for no limit")
	}
	if f.MaxMinutes > 0 && f.MaxMinutes < f.MinMinutes {
		return errors.New("the longest length is shorter than the shortest")
	}
	for _, words := range []*[]string{&f.Include, &f.Exclude} {
		var out []string
		for _, w := range *words {
			w = strings.TrimSpace(w)
			if w == "" || slices.ContainsFunc(out, func(o string) bool { return strings.EqualFold(o, w) }) {
				continue
			}
			if len([]rune(w)) > maxFilterWord {
				return fmt.Errorf("%q is too long for a title filter", w)
			}
			out = append(out, w)
		}
		if len(out) > maxFilterWords {
			return fmt.Errorf("a title filter takes up to %d words", maxFilterWords)
		}
		if out == nil {
			out = []string{}
		}
		*words = out
	}
	return nil
}

// allows reports whether a check may queue e. A video whose length the listing doesn't give
// (some Shorts and streams) passes the length limits.
func (f Filter) allows(e ytdlp.PlaylistEntry) bool {
	if d := e.Duration; d > 0 {
		if f.MinMinutes > 0 && d < float64(f.MinMinutes*60) {
			return false
		}
		if f.MaxMinutes > 0 && d > float64(f.MaxMinutes*60) {
			return false
		}
	}
	title := strings.ToLower(e.Title)
	has := func(w string) bool { return strings.Contains(title, strings.ToLower(w)) }
	if len(f.Include) > 0 && !slices.ContainsFunc(f.Include, has) {
		return false
	}
	return !slices.ContainsFunc(f.Exclude, has)
}

// Target describes a link the UI offers to follow.
type Target struct {
	URL string `json:"url"` // what would be followed (a channel's home page)
	// Channel is set for YouTube channels, which choose their tabs; Tabs is the suggestion.
	Channel bool     `json:"channel"`
	Tabs    []string `json:"tabs"`
	// Followed is the ID of the feed already following it, or "".
	Followed string `json:"followed"`
}

// Found is sent as "feeds:found" when a check queues new videos.
type Found struct {
	FeedID string   `json:"feedId"`
	Title  string   `json:"title"`
	Count  int      `json:"count"`
	Videos []string `json:"videos"` // titles, the first few
}

// Deps connects the manager to the rest of the app.
type Deps struct {
	// List reads up to limit entries of a playlist or channel (0 means all); see ytdlp.List.
	List func(ctx context.Context, link string, limit int) (*ytdlp.ProbeResult, error)
	// Enqueue adds downloads to the queue and reports how many were added.
	Enqueue func(items []queue.NewJob, opts settings.DownloadOptions) (int, error)
	// Archive returns the download archive's entries ("youtube abc123") when "Remember
	// downloaded videos" is on, or nil.
	Archive func() map[string]bool
	// Every is how often feeds are checked; 0 means only when asked.
	Every func() time.Duration
	// Emit delivers an event to the UI.
	Emit func(event string, payload any)
	// Path is where feeds are saved ("" disables saving).
	Path string
}

// Tabs a YouTube channel can be followed by, in the order they are checked.
var channelTabs = []string{"videos", "shorts", "streams"}

// Replaced in tests.
var (
	startDelay = 15 * time.Second // the first look after launch, once tools are detected
	errorRetry = time.Hour        // a failed check is tried again this soon, if that's sooner
	maxWait    = 5 * time.Minute  // the clock is looked at again at least this often (sleep, settings)
	checkGap   = 3 * time.Second  // between two feeds checked in a row
)

const (
	checkLimit    = 50   // newest entries a check of a channel tab reads
	baselineLimit = 1000 // and the baseline: enough that a deletion can't bring an old video back
	maxSeen       = 20000
)

type record struct {
	Feed
	Seen []string `json:"seen"` // video IDs noted, oldest first
	// NewTabs are channel tabs added since the baseline, whose videos are noted, not queued, by
	// the next check.
	NewTabs []string `json:"newTabs,omitempty"`
	// AutoTitle is set while the title is made up from the link; a check names the feed.
	AutoTitle bool `json:"autoTitle,omitempty"`
	seen      map[string]bool
}

func (r *record) note(id string) {
	if !r.seen[id] {
		r.seen[id] = true
		r.Seen = append(r.Seen, id)
	}
}

// Manager owns the followed feeds and checks them.
type Manager struct {
	deps Deps

	mu     sync.Mutex
	feeds  []*record
	want   map[string]bool // asked to be checked now
	noSave bool            // feeds.json exists but couldn't be read, so it is never overwritten
	gen    int64           // snapshots taken

	saveMu   sync.Mutex
	savedGen int64
	emitMu   sync.Mutex // keeps "feeds:update" events in order

	wake   chan struct{}
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New loads the saved feeds.
func New(deps Deps) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{deps: deps, want: map[string]bool{}, wake: make(chan struct{}, 1), ctx: ctx, cancel: cancel}
	m.load()
	return m
}

// Start runs the checks in the background. Call once.
func (m *Manager) Start() {
	m.wg.Add(1)
	go m.loop()
}

// Shutdown stops a check in progress and waits for the loop to end.
func (m *Manager) Shutdown() {
	m.cancel()
	m.wg.Wait()
}

// Kick re-reads the schedule, e.g. after the interval setting changed.
func (m *Manager) Kick() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// ---- public API ----

// List returns every feed in the order they were followed.
func (m *Manager) List() []Feed {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Feed, 0, len(m.feeds))
	for _, r := range m.feeds {
		out = append(out, cloneFeed(r.Feed))
	}
	return out
}

// Inspect says how a link would be followed, and whether it already is.
func (m *Manager) Inspect(raw string) (Target, error) {
	link, err := ytdlp.NormalizeURL(raw)
	if err != nil {
		return Target{}, err
	}
	t := Target{URL: link, Tabs: []string{}}
	if home, tab, ok := ytdlp.YoutubeChannel(link); ok && newestFirst(tab) {
		t.URL, t.Channel = home, true
		if tab == "" || tab == "featured" {
			t.Tabs = []string{"videos"}
		} else {
			t.Tabs = []string{tab}
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.findURLLocked(t.URL); r != nil {
		t.Followed = r.ID
	}
	return t, nil
}

// Follow adds a feed. Its first check, straight away, only notes the videos already there.
func (m *Manager) Follow(nf NewFeed) (Feed, error) {
	t, err := m.Inspect(nf.URL)
	if err != nil {
		return Feed{}, err
	}
	if t.Followed != "" {
		return Feed{}, errors.New("you already follow this. Change its options on the Following page")
	}
	opts := nf.Options
	if err := settings.ValidateOptions(&opts); err != nil {
		return Feed{}, err
	}
	oneVideoOnly(&opts)
	filter := nf.Filter
	if err := filter.clean(); err != nil {
		return Feed{}, err
	}
	if opts.Preset == "custom" {
		return Feed{}, errors.New("a format picked from one video's list can't be used for new uploads. Choose a quality preset")
	}
	var tabs []string
	if t.Channel {
		if len(nf.Tabs) == 0 {
			nf.Tabs = t.Tabs // the tab the link opens
		}
		if tabs, err = cleanTabs(nf.Tabs); err != nil {
			return Feed{}, err
		}
	}
	f := Feed{
		ID: newID(), URL: t.URL, Title: feedTitle(nf, t.Channel), Uploader: strings.TrimSpace(nf.Uploader),
		Tabs: tabs, Options: opts, Filter: filter, RecordLive: nf.RecordLive, AddedAt: now(),
	}
	m.mu.Lock()
	if m.findURLLocked(t.URL) != nil {
		m.mu.Unlock()
		return Feed{}, errors.New("you already follow this")
	}
	auto := strings.TrimSpace(nf.Title) == "" && strings.TrimSpace(nf.Uploader) == ""
	m.feeds = append(m.feeds, &record{Feed: f, Seen: []string{}, AutoTitle: auto, seen: map[string]bool{}})
	m.want[f.ID] = true
	m.mu.Unlock()
	m.changed()
	m.Kick()
	return cloneFeed(f), nil
}

// Unfollow forgets a feed. Downloads it queued stay in the queue.
func (m *Manager) Unfollow(id string) error {
	m.mu.Lock()
	kept := m.feeds[:0]
	found := false
	for _, r := range m.feeds {
		if r.ID == id {
			found = true
			continue
		}
		kept = append(kept, r)
	}
	m.feeds = kept
	delete(m.want, id)
	m.mu.Unlock()
	if !found {
		return errNoFeed
	}
	m.changed()
	return nil
}

// Source is the link to list for browsing a feed: one of a channel's tabs (any of them, not
// only those followed), or the feed's own link when tab is "".
func (m *Manager) Source(id, tab string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.findLocked(id)
	if r == nil {
		return "", errNoFeed
	}
	if len(r.Tabs) == 0 {
		if tab != "" {
			return "", errors.New("only YouTube channels have tabs")
		}
		return r.URL, nil
	}
	if tab == "" {
		tab = r.Tabs[0]
	}
	if !contains(channelTabs, tab) {
		return "", fmt.Errorf("unknown channel tab %q", tab)
	}
	return strings.TrimRight(r.URL, "/") + "/" + tab, nil
}

// SetPaused stops or restarts the automatic checks of a feed. Uploads that appear while it is
// paused are found by the first check after it restarts.
func (m *Manager) SetPaused(id string, paused bool) error {
	err := m.update(id, func(r *record) error {
		r.Paused = paused
		return nil
	})
	if err == nil && !paused {
		m.Kick()
	}
	return err
}

// oneVideoOnly clears the choices that only make sense for one video: picked chapters would cut
// every new upload down to those titles. The audio language stays, since it falls back to the
// default track where a video has none in it.
func oneVideoOnly(o *settings.DownloadOptions) {
	o.SectionStart, o.SectionEnd, o.LiveFromStart, o.WaitForVideo = "", "", false, false
	o.Chapters = []string{}
}

// Edit changes the download options and filter of a feed, and for a channel which tabs are
// checked.
func (m *Manager) Edit(id string, tabs []string, opts settings.DownloadOptions, filter Filter, recordLive bool) (Feed, error) {
	if err := settings.ValidateOptions(&opts); err != nil {
		return Feed{}, err
	}
	if err := filter.clean(); err != nil {
		return Feed{}, err
	}
	if opts.Preset == "custom" {
		return Feed{}, errors.New("choose a quality preset")
	}
	oneVideoOnly(&opts)
	var out Feed
	err := m.update(id, func(r *record) error {
		if len(r.Tabs) > 0 {
			clean, err := cleanTabs(tabs)
			if err != nil {
				return err
			}
			var added []string
			for _, t := range clean {
				if !contains(r.Tabs, t) || contains(r.NewTabs, t) {
					added = append(added, t)
				}
			}
			if len(added) > 0 && !r.Paused {
				// The new tab's videos are all unseen: the next check notes them without queuing.
				m.want[r.ID] = true
			}
			r.Tabs, r.NewTabs = clean, added
		}
		r.Options = opts
		r.Filter = filter
		r.RecordLive = recordLive
		out = cloneFeed(r.Feed)
		return nil
	})
	if err == nil {
		m.Kick()
	}
	return out, err
}

// CheckNow checks a feed as soon as possible, or with id "" every feed that isn't paused.
func (m *Manager) CheckNow(id string) error {
	m.mu.Lock()
	found := false
	for _, r := range m.feeds {
		if r.ID == id || (id == "" && !r.Paused) {
			m.want[r.ID] = true
			found = true
		}
	}
	m.mu.Unlock()
	if !found && id != "" {
		return errNoFeed
	}
	m.Kick()
	return nil
}

var errNoFeed = errors.New("that channel or playlist is no longer followed")

func (m *Manager) update(id string, fn func(*record) error) error {
	m.mu.Lock()
	r := m.findLocked(id)
	if r == nil {
		m.mu.Unlock()
		return errNoFeed
	}
	err := fn(r)
	m.mu.Unlock()
	if err == nil {
		m.changed()
	}
	return err
}

func (m *Manager) findLocked(id string) *record {
	for _, r := range m.feeds {
		if r.ID == id {
			return r
		}
	}
	return nil
}

func (m *Manager) findURLLocked(link string) *record {
	for _, r := range m.feeds {
		if strings.EqualFold(r.URL, link) {
			return r
		}
	}
	return nil
}

// ---- checking ----

func (m *Manager) loop() {
	defer m.wg.Done()
	wait := startDelay
	for {
		t := time.NewTimer(wait)
		select {
		case <-m.ctx.Done():
			t.Stop()
			return
		case <-t.C:
		case <-m.wake:
			t.Stop()
		}
		for checked := 0; ; checked++ {
			id := m.due()
			if id == "" || m.ctx.Err() != nil {
				break
			}
			if checked > 0 {
				select {
				case <-m.ctx.Done():
					return
				case <-time.After(checkGap):
				}
			}
			m.check(id)
		}
		wait = m.untilNext()
	}
}

// due returns the next feed to check now, or "".
func (m *Manager) due() string {
	every := m.deps.Every()
	t := now()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.feeds {
		if m.want[r.ID] {
			return r.ID
		}
	}
	for _, r := range m.feeds {
		if at := nextCheck(r, every); !at.IsZero() && !at.After(t) {
			return r.ID
		}
	}
	return ""
}

// untilNext is how long the loop may sleep before a check is due.
func (m *Manager) untilNext() time.Duration {
	every := m.deps.Every()
	t := now()
	wait := maxWait
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.feeds {
		if at := nextCheck(r, every); !at.IsZero() {
			wait = min(wait, max(time.Second, at.Sub(t)))
		}
	}
	return wait
}

// nextCheck is when a feed is next checked by itself, or zero for never. A feed that hasn't taken
// its baseline is checked straight away (even paused: it queues nothing), as is one with a tab
// added, and a failed check is tried again within errorRetry.
func nextCheck(r *record, every time.Duration) time.Time {
	f := r.Feed
	if f.Checking {
		return time.Time{}
	}
	if !f.Ready || (len(r.NewTabs) > 0 && !f.Paused) {
		if f.Error != nil {
			return f.LastCheck.Add(errorRetry)
		}
		return f.AddedAt
	}
	if f.Paused || every <= 0 {
		return time.Time{}
	}
	if f.Error != nil && errorRetry < every {
		every = errorRetry
	}
	return f.LastCheck.Add(every)
}

// check lists one feed and queues what's new, or notes what's there for the baseline.
func (m *Manager) check(id string) {
	m.mu.Lock()
	delete(m.want, id)
	r := m.findLocked(id)
	if r == nil {
		m.mu.Unlock()
		return
	}
	r.Checking = true
	f := cloneFeed(r.Feed)
	newTabs := append([]string{}, r.NewTabs...)
	m.mu.Unlock()
	m.emit() // Checking isn't saved, so nothing on disk changes

	entries, name, err := m.listFeed(f, newTabs)
	var archive map[string]bool
	if err == nil && f.Ready && !f.Options.IgnoreArchive && m.deps.Archive != nil {
		archive = m.deps.Archive()
	}

	m.mu.Lock()
	r = m.findLocked(id)
	if r == nil || m.ctx.Err() != nil {
		// Unfollowed meanwhile, or the app is closing (a cancelled listing isn't a failure).
		if r != nil {
			r.Checking = false
		}
		m.mu.Unlock()
		m.changed()
		return
	}
	r.Checking = false
	r.LastCheck = now()
	if err != nil {
		r.Error = toAppError(err)
		m.mu.Unlock()
		m.changed()
		return
	}
	r.Error = nil
	if r.AutoTitle && name != "" {
		r.Title, r.AutoTitle = name, false
	}
	// What's there at the baseline, or on a tab added since, is old: it is noted, not queued.
	var fresh, live []ytdlp.PlaylistEntry
	var skip []string
	for _, it := range entries {
		e := it.PlaylistEntry
		switch {
		case r.seen[e.ID]:
		case waiting(e) && f.RecordLive && f.Ready && !contains(newTabs, it.tab) && f.Filter.allows(e) && soon(e):
			live = append(live, e)
		case waiting(e):
		case !f.Ready || contains(newTabs, it.tab), archive[strings.ToLower(e.Extractor)+" "+e.ID], !f.Filter.allows(e):
			skip = append(skip, e.ID)
		default:
			fresh = append(fresh, e)
		}
	}
	if !f.Ready {
		for _, id := range skip {
			r.note(id)
		}
		r.Ready, r.LastNew = true, 0
		r.NewTabs = without(r.NewTabs, newTabs)
		r.trim()
		m.mu.Unlock()
		m.changed()
		return
	}
	m.mu.Unlock()

	added := 0
	if len(fresh) > 0 {
		added, err = m.deps.Enqueue(jobsFor(f, fresh, newestFirstLink(f.URL)), f.Options)
	}
	var liveErr error
	if len(live) > 0 && err == nil {
		// Waited for, then recorded from the beginning (a check can come an hour in).
		o := f.Options
		o.WaitForVideo, o.LiveFromStart = true, true
		// A failure here leaves the uploads above queued: they are noted all the same, and only
		// the streams are tried again next time.
		if n, lerr := m.deps.Enqueue(jobsFor(f, live, newestFirstLink(f.URL)), o); lerr == nil {
			added += n
			fresh = append(fresh, live...)
		} else {
			liveErr = lerr
		}
	}

	m.mu.Lock()
	if r = m.findLocked(id); r == nil {
		m.mu.Unlock()
		m.changed()
		return
	}
	if err != nil {
		// Nothing was noted, so the next check tries the same videos again.
		r.Error = toAppError(err)
		m.mu.Unlock()
		m.changed()
		return
	}
	for _, e := range fresh {
		r.note(e.ID)
	}
	for _, id := range skip {
		r.note(id)
	}
	r.NewTabs = without(r.NewTabs, newTabs)
	r.trim()
	if liveErr != nil {
		r.Error = toAppError(liveErr)
	}
	r.LastNew = added
	r.Queued += added
	if added > 0 {
		r.LastFound = now()
	}
	title := r.Title
	m.mu.Unlock()
	m.changed()

	if added > 0 && m.deps.Emit != nil {
		found := Found{FeedID: id, Title: title, Count: added, Videos: []string{}}
		for _, e := range fresh {
			if len(found.Videos) == 3 {
				break
			}
			found.Videos = append(found.Videos, e.Title)
		}
		m.deps.Emit("feeds:found", found)
	}
}

// waiting reports whether a video hasn't happened yet: a scheduled or running live stream is
// left unnoted, so it is queued once it's over.
func waiting(e ytdlp.PlaylistEntry) bool {
	return e.LiveStatus == "is_upcoming" || e.LiveStatus == "is_live"
}

// soonWindow is how far ahead a scheduled stream is recorded from: one weeks away would sit in
// the queue all that time (and YouTube lists placeholders months ahead).
const soonWindow = 48 * time.Hour

// soon reports whether a stream is live, or due within soonWindow. One whose start the listing
// doesn't give isn't: it is taken once it's over, as without RecordLive.
func soon(e ytdlp.PlaylistEntry) bool {
	if e.LiveStatus == "is_live" {
		return true
	}
	return e.ReleaseTimestamp > 0 && time.Unix(e.ReleaseTimestamp, 0).Before(now().Add(soonWindow))
}

// item is a listed video and the channel tab it was found on ("" when not a channel).
type item struct {
	ytdlp.PlaylistEntry
	tab string
}

// listFeed reads the videos of f: each chosen tab of a channel, or its URL. Tabs being added
// (newTabs) are read as deep as a baseline. name is what the site calls the feed: the channel's
// name, or the playlist's title.
func (m *Manager) listFeed(f Feed, newTabs []string) (out []item, name string, err error) {
	tabs := f.Tabs
	if len(tabs) == 0 {
		tabs = []string{""}
	}
	limit := 0
	if newestFirstLink(f.URL) {
		limit = checkLimit
		if !f.Ready {
			limit = baselineLimit
		}
	}
	seen := map[string]bool{}
	nested := 0
	for _, tab := range tabs {
		src := f.URL
		if tab != "" {
			src = strings.TrimRight(f.URL, "/") + "/" + tab
		}
		n := limit
		if n > 0 && contains(newTabs, tab) {
			n = baselineLimit
		}
		res, err := m.deps.List(m.ctx, src, n)
		if err != nil {
			var ae *apperr.AppError
			if len(tabs) > 1 && errors.As(err, &ae) && ae.Code == "no_tab" {
				continue // a channel without Shorts, say, while Videos is checked too
			}
			return nil, "", err
		}
		if res.Kind != "playlist" || res.Playlist == nil {
			return nil, "", apperr.New("not_a_feed", "This is a single video",
				"Only channels and playlists can be followed.", "Follow the channel or playlist the video belongs to.", apperr.ActionNone)
		}
		if name == "" {
			name = res.Playlist.Title
			if tab != "" {
				name = res.Playlist.Uploader
			}
		}
		for _, e := range res.Playlist.Entries {
			switch {
			case e.IsPlaylist:
				nested++
			case e.Unavailable || e.ID == "" || seen[e.ID] || !usable(e.URL):
			default:
				seen[e.ID] = true
				out = append(out, item{e, tab})
			}
		}
	}
	if len(out) == 0 && nested > 0 {
		return nil, "", apperr.New("only_playlists", "This page lists playlists",
			"It lists playlists rather than videos, so there are no uploads to follow here.",
			"Open one of the playlists and follow that, or follow the channel's main page.", apperr.ActionNone)
	}
	return out, strings.TrimSpace(name), nil
}

func usable(link string) bool {
	_, err := ytdlp.NormalizeURL(link)
	return err == nil && strings.HasPrefix(link, "http")
}

// jobsFor turns new entries into downloads, oldest first: a channel's tabs list the newest at
// the top. They are filed under the feed's name, so "own folder" puts them in one.
func jobsFor(f Feed, entries []ytdlp.PlaylistEntry, newestFirst bool) []queue.NewJob {
	jobs := make([]queue.NewJob, 0, len(entries))
	for i := range entries {
		e := entries[i]
		if newestFirst {
			e = entries[len(entries)-1-i]
		}
		up := e.Uploader
		if up == "" {
			up = f.Uploader
		}
		jobs = append(jobs, queue.NewJob{URL: e.URL, Title: e.Title, Uploader: up, Thumbnail: e.Thumbnail, Duration: e.Duration, PlaylistTitle: f.Title})
	}
	return jobs
}

func (r *record) trim() {
	if over := len(r.Seen) - maxSeen; over > 0 {
		for _, id := range r.Seen[:over] {
			delete(r.seen, id)
		}
		r.Seen = append([]string{}, r.Seen[over:]...)
	}
}

// ---- links ----

// newestFirstLink reports whether a feed lists the newest uploads first: a YouTube channel (its
// tabs) or a TikTok profile.
func newestFirstLink(link string) bool {
	if _, tab, ok := ytdlp.YoutubeChannel(link); ok && newestFirst(tab) {
		return true
	}
	return tiktok.IsProfile(link)
}

// newestFirst reports whether a channel tab lists the newest uploads first.
func newestFirst(tab string) bool {
	return tab == "" || tab == "featured" || contains(channelTabs, tab)
}

func cleanTabs(tabs []string) ([]string, error) {
	var out []string
	for _, t := range channelTabs {
		if contains(tabs, t) {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("choose at least one of Videos, Shorts or Live")
	}
	return out, nil
}

// feedTitle names a feed. A channel tab's own title is "Name - Videos", so a channel is named
// after its uploader.
func feedTitle(nf NewFeed, channel bool) string {
	if t := strings.TrimSpace(nf.Uploader); channel && t != "" {
		return t
	}
	if t := strings.TrimSpace(nf.Title); t != "" {
		return t
	}
	if u, err := url.Parse(nf.URL); err == nil {
		return strings.TrimPrefix(u.Host, "www.") + u.Path
	}
	return nf.URL
}

// ---- events and saving ----

// changed tells the UI and saves.
func (m *Manager) changed() {
	m.emit()
	m.save()
}

// emit sends the list to the page without saving it.
func (m *Manager) emit() {
	if m.deps.Emit != nil {
		// The list is read under emitMu, so a later event always carries a later list.
		m.emitMu.Lock()
		m.deps.Emit("feeds:update", m.List())
		m.emitMu.Unlock()
	}
}

type persisted struct {
	Version int       `json:"version"`
	Feeds   []*record `json:"feeds"`
}

// writeFile is replaced in tests.
var writeFile = settings.WriteFileAtomic

func (m *Manager) save() {
	if m.deps.Path == "" {
		return
	}
	m.mu.Lock()
	if m.noSave {
		m.mu.Unlock()
		return
	}
	m.gen++
	gen := m.gen
	p := persisted{Version: 1, Feeds: make([]*record, 0, len(m.feeds))}
	for _, r := range m.feeds {
		c := &record{Feed: cloneFeed(r.Feed), Seen: append([]string{}, r.Seen...),
			NewTabs: append([]string(nil), r.NewTabs...), AutoTitle: r.AutoTitle}
		c.Checking = false
		p.Feeds = append(p.Feeds, c)
	}
	m.mu.Unlock()

	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	if gen <= m.savedGen {
		return
	}
	raw, err := json.Marshal(p)
	if err == nil {
		err = writeFile(m.deps.Path, raw, 0o600)
	}
	if err == nil {
		m.savedGen = gen
	}
	// A failed write (antivirus holding the file) is retried by the next change; what is in
	// memory stays right meanwhile.
}

func (m *Manager) load() {
	if m.deps.Path == "" {
		return
	}
	raw, err := settings.ReadFileRetry(m.deps.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		m.noSave = true
		return
	}
	var p persisted
	if json.Unmarshal(raw, &p) != nil {
		_ = os.Rename(m.deps.Path, m.deps.Path+".corrupt")
		return
	}
	for _, r := range p.Feeds {
		if r == nil || r.ID == "" || r.URL == "" {
			continue
		}
		r.Checking = false
		if r.Tabs == nil {
			r.Tabs = []string{}
		}
		_ = r.Filter.clean() // feeds from before filters have none: empty lists, not null
		if r.Seen == nil {
			r.Seen = []string{}
		}
		r.seen = make(map[string]bool, len(r.Seen))
		for _, id := range r.Seen {
			r.seen[id] = true
		}
		m.feeds = append(m.feeds, r)
	}
}

// ---- helpers ----

// now is the wall clock without Go's monotonic reading: check times are compared with times read
// back from disk, and the monotonic clock may not count the time the computer was asleep.
var now = func() time.Time { return time.Now().Round(0) }

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func toAppError(err error) *apperr.AppError {
	var ae *apperr.AppError
	if errors.As(err, &ae) {
		return ae
	}
	return apperr.New("follow_failed", "Couldn't check for new videos", err.Error(), "", apperr.ActionNone)
}

// without returns s minus the values in drop.
func without(s, drop []string) []string {
	var out []string
	for _, v := range s {
		if !contains(drop, v) {
			out = append(out, v)
		}
	}
	return out
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func cloneFeed(f Feed) Feed {
	f.Tabs = append([]string{}, f.Tabs...)
	f.Filter.Include = append([]string{}, f.Filter.Include...)
	f.Filter.Exclude = append([]string{}, f.Filter.Exclude...)
	f.Options.Subtitles.Langs = append([]string{}, f.Options.Subtitles.Langs...)
	f.Options.SponsorBlock = append([]string{}, f.Options.SponsorBlock...)
	if f.Error != nil {
		e := *f.Error
		f.Error = &e
	}
	return f
}
