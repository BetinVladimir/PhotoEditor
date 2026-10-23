package gateway

import (
	"errors"
	"log/slog"
	"sync"
	"time"
)

// Closed diagnostic vocabulary only: no error messages, SQL, identities, paths,
// capability strings or customer data. Separate setter and terminal records
// preserve the actual abort reason if a setter previously reported an error.
type streamAuthorityObservation struct {
	mu               sync.Mutex
	terminal, setter bool
}
type streamAuthorityFailure struct {
	reason             string
	initial            bool
	elapsed, remaining int64
	expired            bool
}

func (e *streamAuthorityFailure) Error() string { return "stream authority unavailable" }
func (e *streamAuthorityFailure) Unwrap() error { return errStreamAuthority }
func authorityFailure(reason string, started, old time.Time) error {
	now := time.Now()
	remaining := int64(0)
	if !old.IsZero() && old.After(started) {
		remaining = old.Sub(started).Milliseconds()
	}
	return &streamAuthorityFailure{reason: reason, initial: old.IsZero(), elapsed: max(0, now.Sub(started).Milliseconds()), remaining: remaining, expired: !old.IsZero() && !now.Before(old)}
}
func (o *streamAuthorityObservation) failure(err error) {
	var value *streamAuthorityFailure
	if errors.As(err, &value) {
		o.emit(value, false)
	}
}
func (o *streamAuthorityObservation) emit(e *streamAuthorityFailure, setter bool) {
	if e == nil || e.elapsed < 0 || e.remaining < 0 || e.elapsed > 9007199254740991 || e.remaining > 9007199254740991 {
		return
	}
	switch e.reason {
	case "context_done", "already_failed", "old_authority_expired", "refresh_budget_expired", "renewal_context_deadline", "renewal_context_cancelled", "renewal_query_failed", "authorization_rejected", "identity_changed", "reported_authority_expired", "authority_window_elapsed", "deadline_set_failed", "deadline_set_unsupported", "abort_deadline_set_failed", "abort_deadline_set_unsupported":
	default:
		return
	}
	o.mu.Lock()
	if (setter && o.setter) || (!setter && o.terminal) {
		o.mu.Unlock()
		return
	}
	if setter {
		o.setter = true
	} else {
		o.terminal = true
	}
	o.mu.Unlock()
	// Logging failure cannot replace the original abort/cancel/result.
	defer func() { _ = recover() }()
	slog.Warn("gateway stream authority observation", "reason", e.reason, "initial", e.initial, "renewal_elapsed_ms", e.elapsed, "prior_remaining_ms", e.remaining, "prior_expired", e.expired, "deadline_setter", setter)
}
