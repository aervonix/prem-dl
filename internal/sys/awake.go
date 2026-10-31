package sys

import (
	"sync"
	"time"
)

// Awake keeps the computer from going to sleep while downloads run. The screen may still turn
// off. Holding starts at once; letting go waits a little, so the gap between one download
// ending and the next starting doesn't let the computer sleep.
type Awake struct {
	mu      sync.Mutex
	held    bool
	want    bool
	linger  time.Duration
	timer   *time.Timer
	release func()
	hold    func() (release func(), err error)
}

// NewAwake returns an Awake that holds nothing yet.
func NewAwake() *Awake { return &Awake{linger: time.Minute, hold: holdAwake} }

// Set asks for the computer to stay awake (on) or not. A failure to hold is not reported:
// downloads work either way. A nil Awake does nothing.
func (w *Awake) Set(on bool) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.want = on
	switch {
	case on && w.timer != nil:
		w.timer.Stop()
		w.timer = nil
	case on && !w.held:
		if release, err := w.hold(); err == nil {
			w.held, w.release = true, release
		}
	case !on && w.held && w.timer == nil:
		w.timer = time.AfterFunc(w.linger, w.letGo)
	}
}

func (w *Awake) letGo() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.timer = nil
	if w.want || !w.held {
		return
	}
	w.release()
	w.held, w.release = false, nil
}

// Close lets go at once.
func (w *Awake) Close() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.want = false
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
	if w.held {
		w.release()
		w.held, w.release = false, nil
	}
}
