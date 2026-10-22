package gateway

import (
	"context"
	"errors"
	"sync"
	"time"
)

var errStreamAuthority = errors.New("stream authority unavailable")

// streamAuthority owns all live write-deadline updates. A renewal must finish
// inside the PREVIOUS authority window; an expired stream can never be revived.
// The setter must support concurrent calls with Write (net/http's connection does).
type streamAuthority struct {
	ctx         context.Context
	cancel      context.CancelFunc
	resolve     func(context.Context) (time.Time, error)
	setDeadline func(time.Time) error
	abortStream func()
	mu          sync.Mutex
	until       time.Time
	failed      bool
	stopped     bool
	done        chan struct{}
}

func newStreamAuthority(parent context.Context, resolve func(context.Context) (time.Time, error), set func(time.Time) error, abortStream func()) (*streamAuthority, error) {
	ctx, cancel := context.WithCancel(parent)
	a := &streamAuthority{ctx: ctx, cancel: cancel, resolve: resolve, setDeadline: set, abortStream: abortStream, done: make(chan struct{})}
	if err := a.refresh(); err != nil {
		cancel()
		return nil, err
	}
	go a.watch()
	return a, nil
}
func (a *streamAuthority) refresh() error {
	a.mu.Lock()
	old := a.until
	failed := a.failed
	a.mu.Unlock()
	if failed || a.ctx.Err() != nil {
		return errStreamAuthority
	}
	limit := time.Now().Add(5 * time.Second)
	if !old.IsZero() && old.Before(limit) {
		limit = old
	}
	if !time.Now().Before(limit) {
		return errStreamAuthority
	}
	check, cancel := context.WithDeadline(a.ctx, limit)
	until, err := a.resolve(check)
	checkErr := check.Err()
	cancel()
	now := time.Now()
	if err != nil || checkErr != nil || a.ctx.Err() != nil || !now.Before(limit) || !now.Before(until) {
		return errStreamAuthority
	}
	if cap := now.Add(5 * time.Second); cap.Before(until) {
		until = cap
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.failed || a.ctx.Err() != nil || (!old.IsZero() && !time.Now().Before(old)) {
		return errStreamAuthority
	}
	a.until = until
	// Preserve existing unsupported-controller behavior; actual HTTP connections
	// implement deadlines. Setter errors do not grant any additional authority.
	_ = a.setDeadline(until)
	return nil
}
func (a *streamAuthority) abort() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.failed && !a.stopped {
		a.failed = true
		a.cancel()
		if a.abortStream != nil {
			a.abortStream()
		}
		_ = a.setDeadline(time.Now())
	}
}
func (a *streamAuthority) watch() {
	defer close(a.done)
	for {
		a.mu.Lock()
		until := a.until
		a.mu.Unlock()
		delay := time.Until(until)
		renew := delay > time.Second
		if renew {
			delay -= time.Second
		}
		timer := time.NewTimer(max(delay, 0))
		select {
		case <-a.ctx.Done():
			timer.Stop()
			a.abort()
			return
		case <-timer.C:
			if !renew || a.refresh() != nil {
				a.abort()
				return
			}
		}
	}
}
func (a *streamAuthority) valid() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.failed && a.ctx.Err() == nil && time.Now().Before(a.until)
}
func (a *streamAuthority) stop() {
	a.mu.Lock()
	a.stopped = true
	a.cancel()
	a.mu.Unlock()
	<-a.done
}
