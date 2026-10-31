package follow

import (
	"testing"

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
	if _, err := m.Edit(f.ID, nil, opts(), Filter{}); err != nil {
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
