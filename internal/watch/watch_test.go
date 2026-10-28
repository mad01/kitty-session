package watch

import (
	"context"
	"sync"
	"testing"
	"time"
)

const (
	interval = 5 * time.Millisecond
	settle   = 60 * time.Millisecond // long enough for several polls to find nothing
	patience = 2 * time.Second
)

// TestEdgesWakeAtTheFirstChangeAndAfterABurst runs the wake rule over a
// fixed sequence of polls: a lone change wakes at once, a run of changing
// polls wakes at its first change and once more at the quiet poll that
// ends it, and a run of one change wakes only once.
func TestEdgesWakeAtTheFirstChangeAndAfterABurst(t *testing.T) {
	e := edges{last: "a"}
	polls := []struct {
		fp   string
		wake bool
	}{
		{"a", false}, // quiet
		{"b", true},  // a lone change: at once
		{"b", false}, // quiet, nothing held
		{"c", true},  // a burst starts: at once
		{"d", false}, // still changing: held
		{"e", false},
		{"e", true},  // the quiet poll that ends it: once for the held changes
		{"e", false}, // nothing left
		{"f", true},  // a lone change again
		{"f", false},
	}
	for i, p := range polls {
		if got := e.observe(p.fp); got != p.wake {
			t.Fatalf("poll %d (%q): wake %v, want %v", i, p.fp, got, p.wake)
		}
	}
}

// fingerprint is a test fingerprint the test moves by hand.
type fingerprint struct {
	mu  sync.Mutex
	val string
}

func (f *fingerprint) get() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.val
}

func (f *fingerprint) set(v string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.val = v
}

func TestChangesDeliversOncePerChange(t *testing.T) {
	fp := &fingerprint{val: "a"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := Changes(ctx, interval, fp.get)

	quiet := func(when string) {
		t.Helper()
		select {
		case <-ch:
			t.Fatalf("a change was reported %s", when)
		case <-time.After(settle):
		}
	}
	wake := func(when string) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(patience):
			t.Fatalf("no change was reported %s", when)
		}
		quiet("twice " + when)
	}

	quiet("before any change")
	fp.set("b")
	wake("after a change")
	fp.set("b")
	quiet("after the same value again")
	fp.set("c")
	wake("after a second change")

	cancel()
	select {
	case _, open := <-ch:
		if open {
			t.Fatal("a change was reported after the context ended")
		}
	case <-time.After(patience):
		t.Fatal("the channel stayed open after the context ended")
	}
}

// TestChangesJoinsEveryFingerprint checks a move in any one fingerprint is
// a change.
func TestChangesJoinsEveryFingerprint(t *testing.T) {
	first, second := &fingerprint{val: "a"}, &fingerprint{val: "x"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := Changes(ctx, interval, first.get, second.get)
	second.set("y")
	select {
	case <-ch:
	case <-time.After(patience):
		t.Fatal("no change was reported after the second fingerprint moved")
	}
}

// TestChangesCoalescesForASlowReceiver checks a burst nobody reads leaves
// one wake in the channel, not one per poll.
func TestChangesCoalescesForASlowReceiver(t *testing.T) {
	fp := &fingerprint{val: "a"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := Changes(ctx, interval, fp.get)
	for _, v := range []string{"b", "c", "d"} {
		fp.set(v)
		time.Sleep(3 * interval) // let a poll see each one while nobody reads
	}
	time.Sleep(3 * interval) // and the quiet poll that ends the burst
	n := 0
	for {
		select {
		case <-ch:
			n++
			continue
		case <-time.After(settle):
		}
		break
	}
	if n != 1 {
		t.Fatalf("got %d wakes for a burst nobody read, want 1", n)
	}
}
