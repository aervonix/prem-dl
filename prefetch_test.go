package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"yt-dlp-gui/internal/queue"
	"yt-dlp-gui/internal/settings"
)

// fakeQueue is a queue whose jobs a test sets, with what the prefetcher analysed.
type fakeQueue struct {
	mu       sync.Mutex
	jobs     []queue.Job
	kept     map[string]bool
	analysed []string
	block    chan struct{} // when set, analyses wait for it (or for being cancelled)
	stopped  []string      // analyses cancelled
}

func (f *fakeQueue) set(jobs ...queue.Job) {
	f.mu.Lock()
	f.jobs = jobs
	f.mu.Unlock()
}

func (f *fakeQueue) prefetcher() *prefetcher {
	return newPrefetcher(
		func() []queue.Job { f.mu.Lock(); defer f.mu.Unlock(); return append([]queue.Job(nil), f.jobs...) },
		func() int { return 2 },
		func(link string) bool { f.mu.Lock(); defer f.mu.Unlock(); return f.kept[link] },
		func(ctx context.Context, link string) {
			f.mu.Lock()
			f.analysed = append(f.analysed, link)
			block := f.block
			f.mu.Unlock()
			if block != nil {
				select {
				case <-block:
				case <-ctx.Done():
					f.mu.Lock()
					f.stopped = append(f.stopped, link)
					f.mu.Unlock()
					return
				}
			}
			f.mu.Lock()
			f.kept[link] = true
			f.mu.Unlock()
		})
}

func jobOf(link string, status queue.Status) queue.Job {
	return queue.Job{URL: link, Status: status, Options: settings.Default().Defaults}
}

const (
	ytA = "https://www.youtube.com/watch?v=aaaaaaaaaaa"
	ytB = "https://www.youtube.com/watch?v=bbbbbbbbbbb"
	ytC = "https://www.youtube.com/watch?v=ccccccccccc"
)

func TestPrefetchPicksTheVideosNextInLine(t *testing.T) {
	waitFor := func(o queue.Job) queue.Job { o.Options.WaitForVideo = true; return o }
	for name, c := range map[string]struct {
		jobs []queue.Job
		want string
	}{
		"nothing running: queued jobs start at once": {[]queue.Job{jobOf(ytA, queue.StatusQueued)}, ""},
		"next in line":                                {[]queue.Job{jobOf(ytB, queue.StatusDownloading), jobOf(ytA, queue.StatusQueued)}, ytA},
		"another site is skipped":                     {[]queue.Job{jobOf(ytB, queue.StatusDownloading), jobOf("https://vimeo.com/1", queue.StatusQueued), jobOf(ytA, queue.StatusQueued)}, ytA},
		"only one per download slot":                  {[]queue.Job{jobOf(ytB, queue.StatusDownloading), jobOf("https://vimeo.com/1", queue.StatusQueued), jobOf("https://vimeo.com/2", queue.StatusQueued), jobOf(ytA, queue.StatusQueued)}, ""},
		"paused jobs aren't next":                     {[]queue.Job{jobOf(ytB, queue.StatusDownloading), jobOf(ytC, queue.StatusPaused), jobOf(ytA, queue.StatusQueued)}, ytA},
		"a stream to wait for is read when it starts": {[]queue.Job{jobOf(ytB, queue.StatusDownloading), waitFor(jobOf(ytA, queue.StatusQueued))}, ""},
	} {
		f := &fakeQueue{kept: map[string]bool{}}
		f.set(c.jobs...)
		if got := f.prefetcher().next(); got != c.want {
			t.Errorf("%s: next = %q, want %q", name, got, c.want)
		}
	}
}

func TestPrefetchAnalysesEachOnce(t *testing.T) {
	f := &fakeQueue{kept: map[string]bool{ytB: true}}
	f.set(jobOf(ytC, queue.StatusDownloading), jobOf(ytB, queue.StatusQueued), jobOf(ytA, queue.StatusQueued))
	p := f.prefetcher()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.run(ctx)
	for range 3 {
		p.wake()
		time.Sleep(20 * time.Millisecond)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	// B was analysed already (by the Download page, say); A once, however often the queue changes.
	if len(f.analysed) != 1 || f.analysed[0] != ytA {
		t.Errorf("analysed %v", f.analysed)
	}
}

func TestPrefetchStopsWhenTheDownloadStarts(t *testing.T) {
	f := &fakeQueue{kept: map[string]bool{}, block: make(chan struct{})}
	f.set(jobOf(ytC, queue.StatusDownloading), jobOf(ytA, queue.StatusQueued))
	p := f.prefetcher()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.run(ctx)
	p.wake()
	deadline := time.Now().Add(2 * time.Second)
	for {
		f.mu.Lock()
		started := len(f.analysed) == 1
		f.mu.Unlock()
		if started {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("A was never analysed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// A's download starts while it is analysed: the analysis stops.
	f.set(jobOf(ytC, queue.StatusDownloading), jobOf(ytA, queue.StatusStarting))
	p.wake()
	for {
		f.mu.Lock()
		stopped := len(f.stopped) == 1
		f.mu.Unlock()
		if stopped {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the analysis went on")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
