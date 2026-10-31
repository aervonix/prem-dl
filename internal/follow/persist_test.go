package follow

import (
	"path/filepath"
	"testing"
)

// A tab added while paused (or before the next check ran) is forgotten by a restart, and
// every video on it is then queued as new.
func TestAddedTabSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "feeds.json")
	fk := newFake()
	m := New(fk.deps(path))
	fk.set(chanVids, "v1")
	f := follow(t, m, NewFeed{URL: chanHome, Tabs: []string{"videos"}})
	m.check(f.ID)
	_ = m.SetPaused(f.ID, true)
	fk.set(chanHome+"/shorts", "s3", "s2", "s1")
	if _, err := m.Edit(f.ID, []string{"shorts", "videos"}, opts(), Filter{}, false); err != nil {
		t.Fatal(err)
	}
	m2 := New(fk.deps(path)) // the app restarts
	_ = m2.SetPaused(f.ID, false)
	m2.check(f.ID)
	if q := fk.lastQueued(); len(q) != 0 {
		t.Errorf("after a restart the added tab's old videos were queued: %v", q)
	}
}

// A pasted link's made-up title is replaced by the first check, unless the app restarts first.
func TestAutoTitleSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "feeds.json")
	fk := newFake()
	m := New(fk.deps(path))
	fk.set(plLink, "a")
	f := follow(t, m, NewFeed{URL: plLink})
	m2 := New(fk.deps(path))
	m2.check(f.ID)
	if got := get(t, m2, f.ID); got.Title != "Listed "+plLink {
		t.Errorf("title %q, want the playlist's name", got.Title)
	}
}

// Chapters picked from one video would cut every new upload down to those titles.
func TestFeedDropsOneVideoChoices(t *testing.T) {
	m := New(newFake().deps(""))
	o := opts()
	o.Chapters, o.WaitForVideo, o.AudioLang = []string{"Intro"}, true, "en"
	f := follow(t, m, NewFeed{URL: plLink, Options: o})
	if len(f.Options.Chapters) != 0 || f.Options.WaitForVideo || f.Options.AudioLang != "en" {
		t.Errorf("followed with %+v", f.Options)
	}
	f, err := m.Edit(f.ID, nil, o, Filter{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Options.Chapters) != 0 || f.Options.WaitForVideo {
		t.Errorf("edited to %+v", f.Options)
	}
}
