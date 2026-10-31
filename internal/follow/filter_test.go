package follow

import (
	"errors"
	"testing"
	"time"

	"yt-dlp-gui/internal/queue"
	"yt-dlp-gui/internal/settings"
	"yt-dlp-gui/internal/ytdlp"
)

func TestFilterAllows(t *testing.T) {
	f := Filter{MinMinutes: 2, MaxMinutes: 120, Include: []string{"episode", "Part"}, Exclude: []string{"rerun"}}
	if err := f.clean(); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		title string
		secs  float64
		want  bool
	}{
		{"Episode 12: the end", 30 * 60, true},
		{"PART 2", 0, true},                         // no length given: passes the limits
		{"Episode 13", 90, false},                   // too short
		{"Episode 14 (4h stream)", 4 * 3600, false}, // too long
		{"A vlog", 10 * 60, false},                  // none of the words
		{"Episode 12 RERUN", 30 * 60, false},        // an excluded word
	} {
		if got := f.allows(ytdlp.PlaylistEntry{Title: c.title, Duration: c.secs}); got != c.want {
			t.Errorf("%q (%vs): %v, want %v", c.title, c.secs, got, c.want)
		}
	}
	if !(Filter{}).allows(ytdlp.PlaylistEntry{Title: "anything", Duration: 1}) {
		t.Error("an empty filter turned a video away")
	}
}

func TestFilterProblems(t *testing.T) {
	for _, f := range []Filter{{MinMinutes: -1}, {MinMinutes: 30, MaxMinutes: 10}, {Include: []string{string(make([]rune, 101))}}} {
		if f.clean() == nil {
			t.Errorf("%+v accepted", f)
		}
	}
	f := Filter{Include: []string{" a ", "A", "", "b"}}
	if err := f.clean(); err != nil || len(f.Include) != 2 || f.Exclude == nil {
		t.Errorf("clean: %+v %v", f, err)
	}
}

// A check queues only what the filter lets through, and notes the rest so they aren't looked at
// again.
func TestCheckQueuesOnlyWhatTheFilterAllows(t *testing.T) {
	fk := newFake()
	m := New(fk.deps(""))
	fk.set(plLink, "old")
	f := follow(t, m, NewFeed{URL: plLink, Filter: Filter{Exclude: []string{"#shorts"}}})
	m.check(f.ID) // the baseline
	short := entry("s1")
	short.Title = "Wait for it #shorts"
	fk.mu.Lock()
	fk.lists[plLink] = append([]ytdlp.PlaylistEntry{entry("new1"), short}, fk.lists[plLink]...)
	fk.mu.Unlock()
	m.check(f.ID)
	if q := fk.lastQueued(); len(q) != 1 || q[0] != "new1" {
		t.Fatalf("queued %v", q)
	}
	// Loosening the filter later doesn't bring the skipped one back.
	if _, err := m.Edit(f.ID, nil, opts(), Filter{}, false); err != nil {
		t.Fatal(err)
	}
	before := fk.batches()
	m.check(f.ID)
	if fk.batches() != before {
		t.Errorf("the filtered video was queued later: %v", fk.lastQueued())
	}
	if got := get(t, m, f.ID); got.Filter.Include == nil || got.Filter.Exclude == nil {
		t.Errorf("filter lists should be empty, not null: %+v", got.Filter)
	}
}

// With RecordLive a scheduled stream is queued at once, waited for and recorded from its start,
// and noted, so its replay isn't queued later.
func TestRecordLive(t *testing.T) {
	fk := newFake()
	m := New(fk.deps(""))
	fk.set(plLink, "old")
	f := follow(t, m, NewFeed{URL: plLink, RecordLive: true})
	m.check(f.ID)
	soon := entry("soon")
	soon.LiveStatus, soon.ReleaseTimestamp = "is_upcoming", now().Add(3*time.Hour).Unix()
	later := entry("later")
	later.LiveStatus = "is_upcoming" // no start given: taken once it is over
	fk.mu.Lock()
	fk.lists[plLink] = append([]ytdlp.PlaylistEntry{soon, later}, fk.lists[plLink]...)
	fk.mu.Unlock()
	m.check(f.ID)
	if q := fk.lastQueued(); len(q) != 1 || q[0] != "soon" {
		t.Fatalf("queued %v", q)
	}
	fk.mu.Lock()
	o := fk.opts[len(fk.opts)-1]
	fk.mu.Unlock()
	if !o.WaitForVideo || !o.LiveFromStart {
		t.Errorf("options %+v", o)
	}
	// The stream ends: its replay was noted with it.
	fk.mu.Lock()
	fk.lists[plLink][0].LiveStatus = "was_live"
	fk.mu.Unlock()
	before := fk.batches()
	m.check(f.ID)
	if fk.batches() != before {
		t.Errorf("the replay was queued too: %v", fk.lastQueued())
	}
	// Without it, a stream waits to be over, as before.
	g := follow(t, m, NewFeed{URL: "https://www.youtube.com/playlist?list=PLother"})
	fk.set("https://www.youtube.com/playlist?list=PLother", "x")
	m.check(g.ID)
	fk.mu.Lock()
	fk.lists["https://www.youtube.com/playlist?list=PLother"] = append([]ytdlp.PlaylistEntry{soon}, fk.lists["https://www.youtube.com/playlist?list=PLother"]...)
	fk.mu.Unlock()
	before = fk.batches()
	m.check(g.ID)
	if fk.batches() != before {
		t.Errorf("queued a stream without RecordLive")
	}
}

// If queuing the streams fails, the uploads queued before them are still noted.
func TestRecordLiveFailureKeepsUploadsNoted(t *testing.T) {
	fk := newFake()
	m := New(fk.deps(""))
	fk.set(plLink, "old")
	f := follow(t, m, NewFeed{URL: plLink, RecordLive: true})
	m.check(f.ID)
	stream := entry("stream")
	stream.LiveStatus = "is_live"
	fk.mu.Lock()
	fk.lists[plLink] = append([]ytdlp.PlaylistEntry{entry("upload"), stream}, fk.lists[plLink]...)
	fk.mu.Unlock()
	// The second Enqueue (the stream) fails.
	calls := 0
	enqueue := m.deps.Enqueue
	m.deps.Enqueue = func(items []queue.NewJob, o settings.DownloadOptions) (int, error) {
		if calls++; calls == 2 {
			return 0, errors.New("the queue is full")
		}
		return enqueue(items, o)
	}
	m.check(f.ID)
	m.deps.Enqueue = enqueue
	before := fk.batches()
	m.check(f.ID)
	if q := fk.lastQueued(); fk.batches() != before+1 || len(q) != 1 || q[0] != "stream" {
		t.Errorf("the next check queued %v (batches %d -> %d), want only the stream", q, before, fk.batches())
	}
}
