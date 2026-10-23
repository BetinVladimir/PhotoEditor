package gateway

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

var errStreamAuthority = errors.New("stream authority unavailable")
var errStreamIdentityChanged = errors.New("stream identity changed")

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
	observation streamAuthorityObservation
}

func newStreamAuthority(parent context.Context, resolve func(context.Context) (time.Time, error), set func(time.Time) error, abortStream func()) (*streamAuthority, error) {
	ctx, cancel := context.WithCancel(parent)
	a := &streamAuthority{ctx: ctx, cancel: cancel, resolve: resolve, setDeadline: set, abortStream: abortStream, done: make(chan struct{})}
	if err := a.refresh(); err != nil {
		cancel()
		a.observation.failure(err)
		return nil, err
	}
	go a.watch()
	return a, nil
}
func (a *streamAuthority) refresh() error {
	started := time.Now()
	a.mu.Lock()
	old := a.until
	failed := a.failed
	a.mu.Unlock()
	if failed {
		return authorityFailure("already_failed", started, old)
	}
	if a.ctx.Err() != nil {
		return authorityFailure("context_done", started, old)
	}
	limit := time.Now().Add(5 * time.Second)
	if !old.IsZero() && old.Before(limit) {
		limit = old
	}
	if !time.Now().Before(limit) {
		if !old.IsZero() {
			return authorityFailure("old_authority_expired", started, old)
		}
		return authorityFailure("refresh_budget_expired", started, old)
	}
	check, cancel := context.WithDeadline(a.ctx, limit)
	until, err := a.resolve(check)
	checkErr := check.Err()
	cancel()
	now := time.Now()
	if a.ctx.Err() != nil {
		return authorityFailure("context_done", started, old)
	}
	if errors.Is(checkErr, context.DeadlineExceeded) {
		return authorityFailure("renewal_context_deadline", started, old)
	}
	if errors.Is(checkErr, context.Canceled) {
		return authorityFailure("renewal_context_cancelled", started, old)
	}
	if errors.Is(err, errStreamIdentityChanged) {
		return authorityFailure("identity_changed", started, old)
	}
	if errors.Is(err, ErrCapability) {
		return authorityFailure("authorization_rejected", started, old)
	}
	if err != nil {
		return authorityFailure("renewal_query_failed", started, old)
	}
	if !now.Before(limit) {
		if !old.IsZero() {
			return authorityFailure("old_authority_expired", started, old)
		}
		return authorityFailure("refresh_budget_expired", started, old)
	}
	if !now.Before(until) {
		return authorityFailure("reported_authority_expired", started, old)
	}
	if cap := now.Add(5 * time.Second); cap.Before(until) {
		until = cap
	}
	a.mu.Lock()
	if a.failed {
		a.mu.Unlock()
		return authorityFailure("already_failed", started, old)
	}
	if a.ctx.Err() != nil {
		a.mu.Unlock()
		return authorityFailure("context_done", started, old)
	}
	if !old.IsZero() && !time.Now().Before(old) {
		a.mu.Unlock()
		return authorityFailure("old_authority_expired", started, old)
	}
	a.until = until
	// Preserve existing unsupported-controller behavior; actual HTTP connections
	// implement deadlines. Setter errors do not grant any additional authority.
	setterErr := a.setDeadline(until)
	a.mu.Unlock()
	if setterErr != nil {
		reason := "deadline_set_failed"
		if errors.Is(setterErr, http.ErrNotSupported) {
			reason = "deadline_set_unsupported"
		}
		value := authorityFailure(reason, started, old).(*streamAuthorityFailure)
		a.observation.emit(value, true)
	}
	return nil
}
func (a *streamAuthority) abort() bool {
	a.mu.Lock()
	if a.failed || a.stopped {
		a.mu.Unlock()
		return false
	}
	a.failed = true
	a.cancel()
	if a.abortStream != nil {
		a.abortStream()
	}
	old := a.until
	started := time.Now()
	setterErr := a.setDeadline(started)
	a.mu.Unlock()
	if setterErr != nil {
		reason := "abort_deadline_set_failed"
		if errors.Is(setterErr, http.ErrNotSupported) {
			reason = "abort_deadline_set_unsupported"
		}
		a.observation.emit(authorityFailure(reason, started, old).(*streamAuthorityFailure), true)
	}
	return true
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
			// Start with half the live window still available for the DB check.
			// Waiting until its last second makes normal query latency expire a
			// healthy stream. refresh still caps its context at the PREVIOUS
			// deadline: this is scheduling headroom, never an authority grace.
			delay /= 2
		}
		timer := time.NewTimer(max(delay, 0))
		select {
		case <-a.ctx.Done():
			timer.Stop()
			aborted := a.abort()
			if aborted {
				a.observation.failure(authorityFailure("context_done", time.Now(), until))
			}
			return
		case <-timer.C:
			var err error
			if !renew {
				err = authorityFailure("authority_window_elapsed", time.Now(), until)
			} else {
				err = a.refresh()
			}
			if err != nil {
				if a.abort() {
					a.observation.failure(err)
				}
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
