package main

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestRunIntervals(t *testing.T) {
	var fast, slow, block atomic.Int32
	release := make(chan struct{})
	cs := []collector{
		{"fast", fixed(20 * time.Millisecond), func() (interface{}, error) { fast.Add(1); return fast.Load(), nil }},
		{"slow", fixed(time.Hour), func() (interface{}, error) { slow.Add(1); return 1, nil }},
		{"block", fixed(time.Millisecond), func() (interface{}, error) { block.Add(1); <-release; return 1, nil }},
	}
	h := newHub()
	stop := make(chan struct{})
	go h.run(cs, 5*time.Millisecond, stop)
	time.Sleep(300 * time.Millisecond)
	close(stop)
	close(release)
	// fast ~15 runs in 300ms while block is stuck in its first run; slow ran once (at start).
	if n := fast.Load(); n < 5 || n > 20 {
		t.Errorf("fast ran %d times", n)
	}
	if slow.Load() != 1 || block.Load() != 1 {
		t.Errorf("slow=%d block=%d, want 1 and 1 (in-flight guard)", slow.Load(), block.Load())
	}
	var got int32
	if !h.topic("fast", &got) || got < 5 {
		t.Errorf("fast not published: %d", got)
	}
}

// Shortening an interval right after a run applies at once, not after the old deadline.
func TestRunIntervalShortened(t *testing.T) {
	var every atomic.Int64
	every.Store(int64(time.Hour))
	var n atomic.Int32
	cs := []collector{{"dyn", func() time.Duration { return time.Duration(every.Load()) }, func() (interface{}, error) { n.Add(1); return 1, nil }}}
	stop := make(chan struct{})
	defer close(stop)
	go newHub().run(cs, 5*time.Millisecond, stop)
	time.Sleep(50 * time.Millisecond)
	every.Store(int64(20 * time.Millisecond))
	time.Sleep(100 * time.Millisecond)
	if got := n.Load(); got < 2 {
		t.Fatalf("ran %d times; the 1h deadline was kept", got)
	}
}
