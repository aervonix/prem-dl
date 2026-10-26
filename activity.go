package main

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"yt-dlp-gui/internal/queue"
	"yt-dlp-gui/internal/sys"
)

// activity follows the queue for what the app shows outside its window: the taskbar button's
// progress bar, the tray icon's tooltip, and a Windows notification when downloads finish. It is
// fed the same changes as the page (queue.Deps.Observe).
//
// Progress is for the current batch: every job that waited or ran since the queue was last idle.
// Finished ones count as done, so the bar fills up instead of jumping back as jobs leave it.
type activity struct {
	mu       sync.Mutex
	jobs     map[string]queue.Job
	batch    map[string]bool
	finished []queue.Job // finishes waiting to be reported together
	timer    *time.Timer

	gather   time.Duration          // finishes this close together become one notification
	onChange func(overview)         // after every change, outside the lock
	onFinish func(jobs []queue.Job) // downloads finished (completed or failed, never cancelled)
}

// overview is the queue at a glance.
type overview struct {
	Downloading, Queued, Paused int // across the whole queue
	Failed                      int // in the current batch
	Batch                       bool
	Running                     bool    // the batch still has jobs queued or downloading
	Fraction                    float64 // the batch's progress, 0..1; -1 while nothing is known
}

func newActivity(jobs []queue.Job, gather time.Duration, onChange func(overview), onFinish func([]queue.Job)) *activity {
	a := &activity{jobs: map[string]queue.Job{}, batch: map[string]bool{}, gather: gather, onChange: onChange, onFinish: onFinish}
	for _, j := range jobs {
		a.jobs[j.ID] = j
		if pending(j.Status) && j.WaitUntil.IsZero() {
			a.batch[j.ID] = true
		}
	}
	return a
}

func running(s queue.Status) bool {
	return s == queue.StatusStarting || s == queue.StatusDownloading || s == queue.StatusProcessing
}

// pending is queued or running: the job still has work to do.
func pending(s queue.Status) bool { return s == queue.StatusQueued || running(s) }

func ended(s queue.Status) bool { return s == queue.StatusCompleted || s == queue.StatusFailed }

// observe takes a batch of changes from the queue.
func (a *activity) observe(changed []queue.Job, removed []string) {
	a.mu.Lock()
	var fresh []queue.Job
	for _, j := range changed {
		prev, known := a.jobs[j.ID]
		a.jobs[j.ID] = j
		switch {
		case !j.WaitUntil.IsZero():
			// Waiting hours for a scheduled stream would hold the taskbar's progress bar up.
			delete(a.batch, j.ID)
		case pending(j.Status):
			a.batch[j.ID] = true
		case j.Status == queue.StatusCancelled:
			delete(a.batch, j.ID)
		}
		// Only finishes seen happening: jobs that were already done when the app started stay quiet.
		if known && ended(j.Status) && !ended(prev.Status) {
			fresh = append(fresh, j)
		}
	}
	for _, id := range removed {
		delete(a.jobs, id)
		delete(a.batch, id)
	}
	if len(fresh) > 0 && a.onFinish != nil {
		a.finished = append(a.finished, fresh...)
		if a.timer == nil {
			a.timer = time.AfterFunc(a.gather, a.report)
		} else {
			a.timer.Reset(a.gather)
		}
	}
	ov := a.overviewLocked()
	a.mu.Unlock()
	if a.onChange != nil {
		a.onChange(ov)
	}
}

func (a *activity) report() {
	a.mu.Lock()
	jobs := a.finished
	a.finished = nil
	a.mu.Unlock()
	if len(jobs) > 0 {
		a.onFinish(jobs)
	}
}

func (a *activity) current() overview {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.overviewLocked()
}

// overviewLocked summarises the queue, and ends the batch once nothing in it is left to do.
func (a *activity) overviewLocked() overview {
	var ov overview
	for _, j := range a.jobs {
		switch {
		case running(j.Status) && j.WaitUntil.IsZero():
			ov.Downloading++
		case j.Status == queue.StatusQueued:
			ov.Queued++
		case j.Status == queue.StatusPaused:
			ov.Paused++
		}
	}
	var sum float64
	var left, paused, done, known int
	for id := range a.batch {
		j, ok := a.jobs[id]
		if !ok {
			delete(a.batch, id)
			continue
		}
		switch {
		case ended(j.Status):
			sum++
			done++
			if j.Status == queue.StatusFailed {
				ov.Failed++
			}
			continue
		case pending(j.Status):
			left++
		case j.Status == queue.StatusPaused:
			paused++
		}
		if j.Percent >= 0 {
			sum += j.Percent / 100
			known++
		}
	}
	if left == 0 && paused == 0 {
		clear(a.batch)
		ov.Failed = 0
		return ov
	}
	ov.Batch, ov.Running = true, left > 0
	ov.Fraction = -1
	if known > 0 || done > 0 {
		ov.Fraction = min(1, sum/float64(len(a.batch)))
	}
	return ov
}

// taskbarProgress is what the taskbar button shows for ov: green while the batch runs (red once
// something in it failed), yellow when all that's left is paused, nothing when it's over.
func taskbarProgress(ov overview) (sys.ProgressState, float64) {
	switch {
	case !ov.Batch:
		return sys.ProgressNone, 0
	case !ov.Running:
		return sys.ProgressPaused, max(0, ov.Fraction)
	case ov.Fraction < 0:
		return sys.ProgressIndeterminate, 0
	case ov.Failed > 0:
		return sys.ProgressError, ov.Fraction
	default:
		return sys.ProgressNormal, ov.Fraction
	}
}

// trayTooltip is the tray icon's tooltip for ov.
func trayTooltip(ov overview) string {
	var parts []string
	add := func(n int, what string) {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, what))
		}
	}
	add(ov.Downloading, "downloading")
	add(ov.Queued, "queued")
	add(ov.Paused, "paused")
	add(ov.Failed, "failed")
	if len(parts) == 0 {
		return "Prem-dl"
	}
	line := strings.Join(parts, ", ")
	if ov.Batch && ov.Fraction >= 0 {
		line += fmt.Sprintf(" (%d%%)", int(ov.Fraction*100))
	}
	return "Prem-dl\n" + line
}
