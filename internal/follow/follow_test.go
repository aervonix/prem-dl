package follow

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"yt-dlp-gui/internal/apperr"
	"yt-dlp-gui/internal/queue"
	"yt-dlp-gui/internal/settings"
	"yt-dlp-gui/internal/ytdlp"
)

// fake stands in for yt-dlp and the queue.
type fake struct {
	mu      sync.Mutex
	lists   map[string][]ytdlp.PlaylistEntry // by link
	errs    map[string]error
	limits  map[string]int // the limit each link was last listed with
	queued  [][]queue.NewJob
	opts    []settings.DownloadOptions // each batch's options
	enqErr  error
	archive map[string]bool
	every   time.Duration
	events  []string
}

func newFake() *fake {
	return &fake{lists: map[string][]ytdlp.PlaylistEntry{}, errs: map[string]error{}, limits: map[string]int{}, every: 6 * time.Hour}
}

func (f *fake) deps(path string) Deps {
	return Deps{
		List: func(ctx context.Context, link string, limit int) (*ytdlp.ProbeResult, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.limits[link] = limit
			if err := f.errs[link]; err != nil {
				return nil, err
			}
			entries, ok := f.lists[link]
			if !ok {
				return &ytdlp.ProbeResult{Kind: "video", Video: &ytdlp.VideoInfo{}}, nil
			}
			return &ytdlp.ProbeResult{Kind: "playlist", Playlist: &ytdlp.PlaylistInfo{
				Title: "Listed " + link, Uploader: "Uploader of " + link, Entries: append([]ytdlp.PlaylistEntry{}, entries...),
			}}, nil
		},
		Enqueue: func(items []queue.NewJob, opts settings.DownloadOptions) (int, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.enqErr != nil {
				return 0, f.enqErr
			}
			f.queued = append(f.queued, items)
			f.opts = append(f.opts, opts)
			return len(items), nil
		},
		Archive: func() map[string]bool { return f.archive },
		Every:   func() time.Duration { return f.every },
		Emit: func(event string, payload any) {
			f.mu.Lock()
			f.events = append(f.events, event)
			f.mu.Unlock()
		},
		Path: path,
	}
}

func (f *fake) set(link string, ids ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var entries []ytdlp.PlaylistEntry
	for _, id := range ids {
		entries = append(entries, entry(id))
	}
	f.lists[link] = entries
}

func (f *fake) lastQueued() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.queued) == 0 {
		return nil
	}
	var ids []string
	for _, j := range f.queued[len(f.queued)-1] {
		ids = append(ids, j.Title)
	}
	return ids
}

func (f *fake) batches() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.queued)
}

func entry(id string) ytdlp.PlaylistEntry {
	return ytdlp.PlaylistEntry{ID: id, Title: id, URL: "https://www.youtube.com/watch?v=" + id, Extractor: "Youtube"}
}

func opts() settings.DownloadOptions { return settings.Default().Defaults }

func follow(t *testing.T, m *Manager, nf NewFeed) Feed {
	t.Helper()
	if nf.Options.Preset == "" {
		nf.Options = opts()
	}
	f, err := m.Follow(nf)
	if err != nil {
		t.Fatalf("Follow: %v", err)
	}
	return f
}

func get(t *testing.T, m *Manager, id string) Feed {
	t.Helper()
	for _, f := range m.List() {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("feed %s not found", id)
	return Feed{}
}

const (
	chanHome = "https://www.youtube.com/@someone"
	chanVids = chanHome + "/videos"
	plLink   = "https://www.youtube.com/playlist?list=PL123"
)

func TestInspect(t *testing.T) {
	m := New(newFake().deps(""))
	cases := []struct {
		link    string
		url     string
		channel bool
		tabs    []string
	}{
		{chanHome, chanHome, true, []string{"videos"}},
		{chanHome + "/featured", chanHome, true, []string{"videos"}},
		{chanHome + "/shorts", chanHome, true, []string{"shorts"}},
		// A channel's Playlists tab isn't newest-first, and isn't a channel feed.
		{chanHome + "/playlists", chanHome + "/playlists", false, []string{}},
		{plLink, plLink, false, []string{}},
	}
	for _, c := range cases {
		tg, err := m.Inspect(c.link)
		if err != nil {
			t.Fatalf("Inspect(%q): %v", c.link, err)
		}
		if tg.URL != c.url || tg.Channel != c.channel || !reflect.DeepEqual(tg.Tabs, c.tabs) || tg.Followed != "" {
			t.Errorf("Inspect(%q) = %+v", c.link, tg)
		}
	}
	if _, err := m.Inspect("-rf"); err == nil {
		t.Error("Inspect accepted something that isn't a link")
	}
}

func TestFollowRefusesDuplicatesAndBadChoices(t *testing.T) {
	m := New(newFake().deps(""))
	f := follow(t, m, NewFeed{URL: chanHome + "/videos", Title: "Someone - Videos", Uploader: "Someone"})
	if f.URL != chanHome || !reflect.DeepEqual(f.Tabs, []string{"videos"}) || f.Title != "Someone" {
		t.Fatalf("followed %+v", f)
	}
	// The same channel by another of its links.
	if _, err := m.Follow(NewFeed{URL: "https://youtube.com/@SOMEONE", Tabs: []string{"shorts"}, Options: opts()}); err == nil {
		t.Error("followed the same channel twice")
	}
	if tg, _ := m.Inspect(chanHome + "/streams"); tg.Followed != f.ID {
		t.Errorf("Inspect didn't report the channel as followed: %+v", tg)
	}
	if _, err := m.Follow(NewFeed{URL: "https://www.youtube.com/@other", Tabs: []string{"community"}, Options: opts()}); err == nil {
		t.Error("followed a channel with no tab chosen")
	}
	custom := opts()
	custom.Preset, custom.FormatID = "custom", "137+140"
	if _, err := m.Follow(NewFeed{URL: plLink, Options: custom}); err == nil {
		t.Error("followed with a format picked from one video")
	}
	// Choices that only make sense for one video are dropped.
	o := opts()
	o.SectionStart, o.LiveFromStart = "1:00", true
	pl := follow(t, m, NewFeed{URL: plLink, Title: "My list", Options: o})
	if pl.Options.SectionStart != "" || pl.Options.LiveFromStart || len(pl.Tabs) != 0 || pl.Title != "My list" {
		t.Errorf("playlist feed %+v", pl)
	}
}

func TestBaselineThenNewUploads(t *testing.T) {
	fk := newFake()
	m := New(fk.deps(""))
	fk.set(chanVids, "c", "b", "a") // newest first
	f := follow(t, m, NewFeed{URL: chanHome, Uploader: "Someone", Tabs: []string{"videos"}})

	m.check(f.ID)
	got := get(t, m, f.ID)
	if !got.Ready || got.Error != nil || fk.batches() != 0 {
		t.Fatalf("baseline: %+v, %d batches queued", got, fk.batches())
	}
	if fk.limits[chanVids] != baselineLimit {
		t.Errorf("baseline listed %d entries, want %d", fk.limits[chanVids], baselineLimit)
	}

	fk.set(chanVids, "e", "d", "c", "b", "a")
	m.check(f.ID)
	if q := fk.lastQueued(); !reflect.DeepEqual(q, []string{"d", "e"}) {
		t.Errorf("queued %v, want the two new ones oldest first", q)
	}
	if fk.limits[chanVids] != checkLimit {
		t.Errorf("check listed %d entries, want %d", fk.limits[chanVids], checkLimit)
	}
	got = get(t, m, f.ID)
	if got.LastNew != 2 || got.Queued != 2 || got.LastFound.IsZero() {
		t.Errorf("after finding two: %+v", got)
	}

	// A video deleted from the top brings an old one into view: it was noted at the baseline.
	fk.set(chanVids, "d", "c", "b", "a")
	m.check(f.ID)
	if fk.batches() != 1 || get(t, m, f.ID).LastNew != 0 {
		t.Errorf("queued again with nothing new: %d batches", fk.batches())
	}
	queuedJob := fk.queued[0][0]
	if queuedJob.PlaylistTitle != "Someone" || queuedJob.PlaylistIndex != 0 || queuedJob.Uploader != "Someone" {
		t.Errorf("queued job %+v; want it filed under the channel, unnumbered", queuedJob)
	}
}

func TestTikTokProfilesAreReadNewestFirst(t *testing.T) {
	const profile = "https://www.tiktok.com/@someone"
	fk := newFake()
	m := New(fk.deps(""))
	fk.set(profile, "b", "a") // newest first, like a channel tab
	f := follow(t, m, NewFeed{URL: profile})
	m.check(f.ID)
	if fk.limits[profile] != baselineLimit {
		t.Errorf("baseline listed %d entries, want %d", fk.limits[profile], baselineLimit)
	}
	fk.set(profile, "d", "c", "b", "a")
	m.check(f.ID)
	if fk.limits[profile] != checkLimit {
		t.Errorf("check listed %d entries, want %d", fk.limits[profile], checkLimit)
	}
	if q := fk.lastQueued(); !reflect.DeepEqual(q, []string{"c", "d"}) {
		t.Errorf("queued %v, want the two new ones oldest first", q)
	}
}

func TestPlaylistsAreReadInFull(t *testing.T) {
	fk := newFake()
	m := New(fk.deps(""))
	fk.set(plLink, "a", "b")
	f := follow(t, m, NewFeed{URL: plLink})
	m.check(f.ID)
	fk.set(plLink, "a", "b", "c", "d") // appended at the end
	m.check(f.ID)
	if fk.limits[plLink] != 0 {
		t.Errorf("playlist listed with limit %d, want all", fk.limits[plLink])
	}
	if q := fk.lastQueued(); !reflect.DeepEqual(q, []string{"c", "d"}) {
		t.Errorf("queued %v, want c, d in playlist order", q)
	}
}

func TestLiveStreamsWaitUntilOver(t *testing.T) {
	fk := newFake()
	m := New(fk.deps(""))
	up := entry("premiere")
	up.LiveStatus = "is_upcoming"
	fk.lists[chanVids] = []ytdlp.PlaylistEntry{up, entry("old")}
	f := follow(t, m, NewFeed{URL: chanHome, Tabs: []string{"videos"}})
	m.check(f.ID) // baseline: the premiere isn't noted

	up.LiveStatus = "is_live"
	fk.lists[chanVids] = []ytdlp.PlaylistEntry{up, entry("old")}
	m.check(f.ID)
	if fk.batches() != 0 {
		t.Fatalf("queued a stream that is still live")
	}
	up.LiveStatus = "was_live"
	fk.lists[chanVids] = []ytdlp.PlaylistEntry{up, entry("old")}
	m.check(f.ID)
	if q := fk.lastQueued(); !reflect.DeepEqual(q, []string{"premiere"}) {
		t.Errorf("queued %v, want the finished stream", q)
	}
}

func TestArchivedVideosAreSkipped(t *testing.T) {
	fk := newFake()
	m := New(fk.deps(""))
	fk.set(plLink, "a")
	f := follow(t, m, NewFeed{URL: plLink})
	m.check(f.ID)
	fk.archive = map[string]bool{"youtube b": true}
	fk.set(plLink, "a", "b", "c")
	m.check(f.ID)
	if q := fk.lastQueued(); !reflect.DeepEqual(q, []string{"c"}) {
		t.Fatalf("queued %v, want only c (b is in the archive)", q)
	}
	// b was noted, so it isn't offered again once the archive is cleared.
	fk.archive = nil
	m.check(f.ID)
	if fk.batches() != 1 {
		t.Errorf("queued b after the archive was cleared")
	}

	// "Download again" ignores the archive.
	o := opts()
	o.IgnoreArchive = true
	if _, err := m.Edit(f.ID, nil, o, Filter{}, false); err != nil {
		t.Fatal(err)
	}
	fk.archive = map[string]bool{"youtube d": true}
	fk.set(plLink, "a", "b", "c", "d")
	m.check(f.ID)
	if q := fk.lastQueued(); !reflect.DeepEqual(q, []string{"d"}) {
		t.Errorf("queued %v, want d despite the archive", q)
	}
}

func TestFailuresKeepVideosForTheNextCheck(t *testing.T) {
	fk := newFake()
	m := New(fk.deps(""))
	fk.set(plLink, "a")
	f := follow(t, m, NewFeed{URL: plLink})
	m.check(f.ID)

	fk.set(plLink, "a", "b")
	fk.enqErr = errors.New("disk full")
	m.check(f.ID)
	if e := get(t, m, f.ID).Error; e == nil || e.Message != "disk full" {
		t.Fatalf("error = %+v", e)
	}
	fk.enqErr = nil
	m.check(f.ID)
	if q := fk.lastQueued(); !reflect.DeepEqual(q, []string{"b"}) {
		t.Errorf("queued %v after the failure cleared, want b", q)
	}
	if get(t, m, f.ID).Error != nil {
		t.Error("a check that worked kept the old error")
	}

	fk.errs[plLink] = apperr.New("network", "No connection", "offline", "", apperr.ActionNone)
	m.check(f.ID)
	if e := get(t, m, f.ID).Error; e == nil || e.Code != "network" {
		t.Errorf("listing error = %+v", e)
	}
}

func TestBaselineFailureIsRetried(t *testing.T) {
	fk := newFake()
	m := New(fk.deps(""))
	fk.errs[plLink] = apperr.New("network", "No connection", "offline", "", apperr.ActionNone)
	f := follow(t, m, NewFeed{URL: plLink})
	m.check(f.ID)
	got := get(t, m, f.ID)
	if got.Ready || got.Error == nil {
		t.Fatalf("after a failed baseline: %+v", got)
	}
	delete(fk.errs, plLink)
	fk.set(plLink, "a")
	m.check(f.ID)
	if got := get(t, m, f.ID); !got.Ready || fk.batches() != 0 {
		t.Errorf("the retried baseline queued something or didn't finish: %+v", got)
	}
}

func TestMissingTabsAreSkipped(t *testing.T) {
	fk := newFake()
	m := New(fk.deps(""))
	noTab := apperr.Classify([]string{"ERROR: [youtube:tab] @someone: This channel does not have a shorts tab"}, errors.New("exit 1"))
	fk.errs[chanHome+"/shorts"] = noTab
	fk.set(chanVids, "a")
	f := follow(t, m, NewFeed{URL: chanHome, Tabs: []string{"videos", "shorts"}})
	m.check(f.ID)
	if got := get(t, m, f.ID); !got.Ready || got.Error != nil {
		t.Errorf("a missing Shorts tab failed the check: %+v", got)
	}

	// With only that tab chosen, the user needs to know.
	only := follow(t, m, NewFeed{URL: "https://www.youtube.com/@someone2", Tabs: []string{"shorts"}})
	fk.errs["https://www.youtube.com/@someone2/shorts"] = noTab
	m.check(only.ID)
	if e := get(t, m, only.ID).Error; e == nil || e.Code != "no_tab" {
		t.Errorf("error = %+v, want no_tab", e)
	}
}

func TestNotAFeed(t *testing.T) {
	fk := newFake()
	m := New(fk.deps(""))
	f := follow(t, m, NewFeed{URL: "https://example.com/video"}) // the fake answers "video"
	m.check(f.ID)
	if e := get(t, m, f.ID).Error; e == nil || e.Code != "not_a_feed" {
		t.Errorf("error = %+v", e)
	}

	pls := follow(t, m, NewFeed{URL: chanHome + "/playlists"})
	fk.lists[chanHome+"/playlists"] = []ytdlp.PlaylistEntry{{ID: "PL1", URL: plLink, IsPlaylist: true}}
	m.check(pls.ID)
	if e := get(t, m, pls.ID).Error; e == nil || e.Code != "only_playlists" {
		t.Errorf("error = %+v", e)
	}
}

func TestAddedTabIsNotedNotQueued(t *testing.T) {
	fk := newFake()
	m := New(fk.deps(""))
	fk.set(chanVids, "v1")
	f := follow(t, m, NewFeed{URL: chanHome, Tabs: []string{"videos"}})
	m.check(f.ID)

	fk.set(chanVids, "v2", "v1")
	fk.set(chanHome+"/shorts", "s2", "s1")
	if _, err := m.Edit(f.ID, []string{"shorts", "videos"}, opts(), Filter{}, false); err != nil {
		t.Fatal(err)
	}
	if id := m.due(); id != f.ID {
		t.Fatalf("a feed with a new tab isn't due: %q", id)
	}
	m.check(f.ID)
	if q := fk.lastQueued(); !reflect.DeepEqual(q, []string{"v2"}) {
		t.Errorf("queued %v, want only the new upload on Videos", q)
	}
	if fk.limits[chanHome+"/shorts"] != baselineLimit || fk.limits[chanVids] != checkLimit {
		t.Errorf("limits %v", fk.limits)
	}
	fk.set(chanHome+"/shorts", "s3", "s2", "s1")
	m.check(f.ID)
	if q := fk.lastQueued(); !reflect.DeepEqual(q, []string{"s3"}) {
		t.Errorf("queued %v, want the new short", q)
	}
	if _, err := m.Edit(f.ID, nil, opts(), Filter{}, false); err == nil {
		t.Error("a channel was left with no tabs")
	}
}

func TestSchedule(t *testing.T) {
	every := 6 * time.Hour
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	rec := func(f Feed) *record { return &record{Feed: f} }
	cases := []struct {
		name string
		r    *record
		want time.Time
	}{
		{"baseline pending", rec(Feed{AddedAt: base}), base},
		{"baseline pending while paused", rec(Feed{AddedAt: base, Paused: true}), base},
		{"baseline failed", rec(Feed{AddedAt: base, LastCheck: base, Error: &apperr.AppError{}}), base.Add(errorRetry)},
		{"ready", rec(Feed{Ready: true, LastCheck: base}), base.Add(every)},
		{"failed check", rec(Feed{Ready: true, LastCheck: base, Error: &apperr.AppError{}}), base.Add(errorRetry)},
		{"paused", rec(Feed{Ready: true, LastCheck: base, Paused: true}), time.Time{}},
		{"checking", rec(Feed{Ready: true, LastCheck: base, Checking: true}), time.Time{}},
		{"new tab", &record{Feed: Feed{Ready: true, LastCheck: base, AddedAt: base.Add(-time.Hour)}, NewTabs: []string{"shorts"}}, base.Add(-time.Hour)},
		{"new tab while paused", &record{Feed: Feed{Ready: true, LastCheck: base, Paused: true}, NewTabs: []string{"shorts"}}, time.Time{}},
	}
	for _, c := range cases {
		if got := nextCheck(c.r, every); !got.Equal(c.want) {
			t.Errorf("%s: next check %v, want %v", c.name, got, c.want)
		}
	}
	if got := nextCheck(rec(Feed{Ready: true, LastCheck: base}), 0); !got.IsZero() {
		t.Errorf("checks are off, yet one is due at %v", got)
	}
	// A failed check waits for the interval when that's shorter than the retry delay.
	if got := nextCheck(rec(Feed{Ready: true, LastCheck: base, Error: &apperr.AppError{}}), 30*time.Minute); !got.Equal(base.Add(30 * time.Minute)) {
		t.Errorf("failed check with a short interval: %v", got)
	}
}

func TestDueOrder(t *testing.T) {
	fk := newFake()
	m := New(fk.deps(""))
	fk.set(plLink, "a")
	a := follow(t, m, NewFeed{URL: plLink})
	m.check(a.ID)
	if id := m.due(); id != "" {
		t.Fatalf("due right after a check: %q", id)
	}
	if err := m.CheckNow(a.ID); err != nil {
		t.Fatal(err)
	}
	if id := m.due(); id != a.ID {
		t.Errorf("Check now didn't make it due: %q", id)
	}
	m.check(a.ID)
	_ = m.SetPaused(a.ID, true)
	_ = m.CheckNow("")
	if id := m.due(); id != "" {
		t.Errorf("Check all included a paused feed: %q", id)
	}
	if err := m.CheckNow("nope"); err == nil {
		t.Error("CheckNow accepted an unknown feed")
	}
}

func TestSavedAndRestored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "feeds.json")
	fk := newFake()
	m := New(fk.deps(path))
	fk.set(plLink, "a", "b")
	f := follow(t, m, NewFeed{URL: plLink, Title: "List"})
	m.check(f.ID)

	m2 := New(fk.deps(path))
	got := get(t, m2, f.ID)
	if !got.Ready || got.Title != "List" || got.Checking {
		t.Fatalf("restored %+v", got)
	}
	fk.set(plLink, "a", "b", "c")
	m2.check(f.ID)
	if q := fk.lastQueued(); !reflect.DeepEqual(q, []string{"c"}) {
		t.Errorf("after a restart queued %v, want c (a and b were noted before)", q)
	}
	if err := m2.Unfollow(f.ID); err != nil {
		t.Fatal(err)
	}
	if n := len(New(fk.deps(path)).List()); n != 0 {
		t.Errorf("%d feeds left after unfollowing", n)
	}
}

func TestSeenIsBounded(t *testing.T) {
	r := &record{seen: map[string]bool{}}
	for i := 0; i < maxSeen+10; i++ {
		r.note(string(rune('a'+i%26)) + time.Duration(i).String())
	}
	r.trim()
	if len(r.Seen) != maxSeen || len(r.seen) != maxSeen {
		t.Errorf("kept %d (%d in the set), want %d", len(r.Seen), len(r.seen), maxSeen)
	}
}

func TestLoopTakesTheBaselineOfANewFeed(t *testing.T) {
	old := startDelay
	startDelay = time.Hour // only the wake-up from Follow can start the check
	defer func() { startDelay = old }()
	fk := newFake()
	m := New(fk.deps(""))
	fk.set(plLink, "a")
	m.Start()
	defer m.Shutdown()
	f := follow(t, m, NewFeed{URL: plLink})
	deadline := time.Now().Add(5 * time.Second)
	for !get(t, m, f.ID).Ready {
		if time.Now().After(deadline) {
			t.Fatal("the baseline never ran")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestPastedLinkIsNamedByTheFirstCheck(t *testing.T) {
	fk := newFake()
	m := New(fk.deps(""))
	fk.set(plLink, "a")
	pl := follow(t, m, NewFeed{URL: plLink})
	if pl.Title != "youtube.com/playlist" {
		t.Errorf("title before the check: %q", pl.Title)
	}
	m.check(pl.ID)
	if got := get(t, m, pl.ID).Title; got != "Listed "+plLink {
		t.Errorf("playlist named %q", got)
	}

	fk.set(chanVids, "a")
	ch := follow(t, m, NewFeed{URL: chanHome})
	m.check(ch.ID)
	if got := get(t, m, ch.ID).Title; got != "Uploader of "+chanVids {
		t.Errorf("channel named %q, want its uploader", got)
	}

	// A name the user saw when following is kept.
	fk.set(plLink+"2", "a")
	named := follow(t, m, NewFeed{URL: plLink + "2", Title: "Mine"})
	m.check(named.ID)
	if got := get(t, m, named.ID).Title; got != "Mine" {
		t.Errorf("renamed to %q", got)
	}
}

func TestSource(t *testing.T) {
	m := New(newFake().deps(""))
	ch := follow(t, m, NewFeed{URL: chanHome, Tabs: []string{"shorts"}})
	pl := follow(t, m, NewFeed{URL: plLink})
	cases := []struct {
		id, tab, want string
		ok            bool
	}{
		{ch.ID, "", chanHome + "/shorts", true}, // the first followed tab
		{ch.ID, "streams", chanHome + "/streams", true},
		{ch.ID, "playlists", "", false},
		{pl.ID, "", plLink, true},
		{pl.ID, "videos", "", false},
		{"nope", "", "", false},
	}
	for _, c := range cases {
		got, err := m.Source(c.id, c.tab)
		if got != c.want || (err == nil) != c.ok {
			t.Errorf("Source(%s, %q) = %q, %v", c.id, c.tab, got, err)
		}
	}
}
