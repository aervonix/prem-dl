package main

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"yt-dlp-gui/internal/follow"
	"yt-dlp-gui/internal/settings"
)

func TestInOrderRunsInParallelAndKeepsOrder(t *testing.T) {
	var running, peak atomic.Int32
	slow := make(chan struct{})
	var mu sync.Mutex
	var used []int
	firstUsed := make(chan struct{})

	go func() {
		// The first result must be used while a later one is still working, not after all are done.
		select {
		case <-firstUsed:
		case <-time.After(5 * time.Second):
			t.Error("the first result waited for the slow one")
		}
		close(slow)
	}()

	inOrder(6, 2, func(i int) int {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		if i == 3 {
			<-slow
		} else {
			time.Sleep(time.Duration(5-i) * 10 * time.Millisecond) // later inputs finish sooner
		}
		running.Add(-1)
		return i * 10
	}, func(i, r int) {
		if r != i*10 {
			t.Errorf("result %d for input %d", r, i)
		}
		mu.Lock()
		used = append(used, i)
		mu.Unlock()
		if i == 0 {
			close(firstUsed)
		}
	})

	for i, v := range used {
		if v != i {
			t.Fatalf("results used out of order: %v", used)
		}
	}
	if len(used) != 6 {
		t.Fatalf("used %d results, want 6", len(used))
	}
	if p := peak.Load(); p > 2 || p < 2 {
		t.Errorf("peak concurrency %d, want 2", p)
	}
}

func TestInOrderWithNothingToDo(t *testing.T) {
	inOrder(0, 4, func(int) int { t.Fatal("work called"); return 0 }, func(int, int) { t.Fatal("use called") })
}

func TestImportFeedsFromTakeout(t *testing.T) {
	store, err := settings.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := &App{store: store, feeds: follow.New(follow.Deps{})}
	path := filepath.Join(t.TempDir(), "subscriptions.csv")
	csv := "Channel Id,Channel Url,Channel Title\n" +
		"UCsXVk37bltHxD1rDPwtNM8Q,http://www.youtube.com/channel/UCsXVk37bltHxD1rDPwtNM8Q,Kurzgesagt\n" +
		"UCHnyfMqiRRG1u-2MsSQLbXA,http://www.youtube.com/channel/UCHnyfMqiRRG1u-2MsSQLbXA,Veritasium\n"
	if err := os.WriteFile(path, []byte(csv), 0o600); err != nil {
		t.Fatal(err)
	}
	res := a.importFeedsFrom(path)
	if res.Error != "" || res.Added != 2 || len(res.Failed) != 0 {
		t.Fatalf("%+v", res)
	}
	feeds := a.feeds.List()
	if len(feeds) != 2 || feeds[0].Title != "Kurzgesagt" || feeds[0].Options.Preset == "" || len(feeds[0].Tabs) != 1 {
		t.Fatalf("%+v", feeds)
	}
	if again := a.importFeedsFrom(path); again.Added != 0 || again.Already != 2 {
		t.Errorf("again: %+v", again)
	}
}
