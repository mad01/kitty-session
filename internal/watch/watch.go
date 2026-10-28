// Package watch wakes a sidebar when something it draws from has changed,
// so it lists again at once instead of at its next poll. It knows nothing
// about what it watches. The host hands it fingerprint functions, strings
// that move when the thing they stand for does (the session store's
// records, the state files), and it polls them.
package watch

import (
	"context"
	"strings"
	"time"
)

// Changes polls the fingerprints every interval and delivers a value when
// their joined result differs from the poll before. The baseline is taken
// before Changes returns, so every change after that is seen. A change
// after a quiet poll is delivered at once. A run of polls that keep
// changing, a burst of writes, delivers nothing more until the first quiet
// poll, which delivers once when anything changed after the first
// delivery. So a lone change waits for nothing and a burst costs the
// receiver two wakes at most. A delivery that finds the previous one still
// unread is dropped, so a slow receiver gets one wake for a burst. The
// channel closes when ctx ends.
func Changes(
	ctx context.Context, interval time.Duration, fingerprints ...func() string,
) <-chan struct{} {
	ch := make(chan struct{}, 1)
	e := edges{last: join(fingerprints)}
	go func() {
		defer close(ch)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			if !e.observe(join(fingerprints)) {
				continue
			}
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}()
	return ch
}

// edges is the rule Changes wakes by: at the first change after a quiet
// poll, then not again while the polls keep changing, then once more at the
// quiet poll that ends the run, when anything changed after the first wake.
type edges struct {
	last    string
	running bool // the previous poll found a change
	held    bool // a change since the last wake has not been delivered
}

// observe takes one poll's fingerprint and reports whether to wake.
func (e *edges) observe(cur string) bool {
	if cur != e.last {
		e.last = cur
		if e.running {
			e.held = true
			return false
		}
		e.running, e.held = true, false
		return true
	}
	e.running = false
	if e.held {
		e.held = false
		return true
	}
	return false
}

// join runs every fingerprint and concatenates the results, each ended by
// a NUL so two fingerprints cannot merge into one another's text.
func join(fingerprints []func() string) string {
	var b strings.Builder
	for _, fp := range fingerprints {
		b.WriteString(fp())
		b.WriteByte(0)
	}
	return b.String()
}
