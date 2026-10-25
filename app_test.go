package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
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
