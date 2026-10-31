package sys

import (
	"sync"
	"testing"
	"time"
)

func TestAwakeHoldsAndLingers(t *testing.T) {
	var mu sync.Mutex
	holds, releases := 0, 0
	w := &Awake{linger: 30 * time.Millisecond, hold: func() (func(), error) {
		mu.Lock()
		holds++
		mu.Unlock()
		return func() { mu.Lock(); releases++; mu.Unlock() }, nil
	}}
	count := func() (int, int) { mu.Lock(); defer mu.Unlock(); return holds, releases }

	w.Set(true)
	w.Set(true)
	if h, r := count(); h != 1 || r != 0 {
		t.Fatalf("holds %d, releases %d", h, r)
	}
	// One download ends and the next starts within the linger: still the one hold.
	w.Set(false)
	w.Set(true)
	time.Sleep(60 * time.Millisecond)
	if h, r := count(); h != 1 || r != 0 {
		t.Fatalf("a gap between downloads let go: holds %d, releases %d", h, r)
	}
	w.Set(false)
	time.Sleep(60 * time.Millisecond)
	if h, r := count(); h != 1 || r != 1 {
		t.Fatalf("after the linger: holds %d, releases %d", h, r)
	}
	w.Set(true)
	w.Close()
	if h, r := count(); h != 2 || r != 2 {
		t.Fatalf("close: holds %d, releases %d", h, r)
	}
}
