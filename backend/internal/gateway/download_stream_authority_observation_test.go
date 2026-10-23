package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type authorityLogBuffer struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (b *authorityLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.Write(p)
}
func (b *authorityLogBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.data.Bytes()...)
}
func (b *authorityLogBuffer) String() string { return string(b.Bytes()) }
func (b *authorityLogBuffer) Len() int       { return len(b.Bytes()) }
func authorityLog(t *testing.T) *authorityLogBuffer {
	t.Helper()
	old := slog.Default()
	var b authorityLogBuffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&b, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &b
}
func authorityReasons(t *testing.T, b *authorityLogBuffer) []string {
	t.Helper()
	var out []string
	for _, raw := range bytes.Split(bytes.TrimSpace(b.Bytes()), []byte("\n")) {
		if len(raw) == 0 {
			continue
		}
		var v map[string]any
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		reason, ok := v["reason"].(string)
		if ok {
			out = append(out, reason)
		}
	}
	return out
}
func TestStreamAuthorityObservationRealTypedFailureCategories(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		resolve    func(context.Context) (time.Time, error)
	}{
		{"query", "renewal_query_failed", func(context.Context) (time.Time, error) { return time.Time{}, errors.New("SECRET_SQL_TOKEN_PATH") }},
		{"identity", "identity_changed", func(context.Context) (time.Time, error) { return time.Time{}, errStreamIdentityChanged }},
		{"rejected", "authorization_rejected", func(context.Context) (time.Time, error) { return time.Time{}, ErrCapability }},
		{"reported_expired", "reported_authority_expired", func(context.Context) (time.Time, error) { return time.Now().Add(-time.Second), nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := authorityLog(t)
			a, err := newStreamAuthority(context.Background(), tc.resolve, func(time.Time) error { return nil }, nil)
			if err == nil || a != nil {
				t.Fatal("failure admitted")
			}
			reasons := authorityReasons(t, b)
			if len(reasons) != 1 || reasons[0] != tc.want || strings.Contains(b.String(), "SECRET_") {
				t.Fatal("untrusted diagnostic or wrong category")
			}
		})
	}
}
func TestStreamAuthorityObservationLateQueryPreservesAbortAndNoRevival(t *testing.T) {
	b := authorityLog(t)
	var abort atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &streamAuthority{ctx: ctx, cancel: cancel, until: time.Now().Add(20 * time.Millisecond), resolve: func(ctx context.Context) (time.Time, error) { <-ctx.Done(); return time.Now().Add(time.Minute), nil }, setDeadline: func(time.Time) error { return nil }, abortStream: func() { abort.Add(1) }}
	err := a.refresh()
	if err == nil || a.valid() {
		t.Fatal("late query revived expired authority")
	}
	a.abort()
	a.observation.failure(err)
	reasons := authorityReasons(t, b)
	if abort.Load() != 1 || len(reasons) != 1 || reasons[0] != "renewal_context_deadline" {
		t.Fatal("late guard category/cancellation wrong")
	}
}
func TestStreamAuthorityObservationSetterErrorDoesNotReplaceExistingOutcome(t *testing.T) {
	for _, setter := range []error{http.ErrNotSupported, io.ErrClosedPipe} {
		t.Run(setter.Error(), func(t *testing.T) {
			b := authorityLog(t)
			a, err := newStreamAuthority(context.Background(), func(context.Context) (time.Time, error) { return time.Now().Add(time.Minute), nil }, func(time.Time) error { return setter }, nil)
			if err != nil {
				t.Fatal("observation changed existing unsupported setter behavior")
			}
			a.stop()
			reasons := authorityReasons(t, b)
			want := "deadline_set_failed"
			if errors.Is(setter, http.ErrNotSupported) {
				want = "deadline_set_unsupported"
			}
			if len(reasons) != 1 || reasons[0] != want {
				t.Fatal("setter diagnostic missing")
			}
		})
	}
}
func TestStreamAuthorityObservationHealthyStopSilent(t *testing.T) {
	b := authorityLog(t)
	a, err := newStreamAuthority(context.Background(), func(context.Context) (time.Time, error) { return time.Now().Add(time.Minute), nil }, func(time.Time) error { return nil }, nil)
	if err != nil {
		t.Fatal(err)
	}
	a.stop()
	if b.Len() != 0 {
		t.Fatal("normal stop labelled failure")
	}
}
func TestStreamAuthorityObservationOneTerminalAndSetterPrivacy(t *testing.T) {
	b := authorityLog(t)
	o := &streamAuthorityObservation{}
	o.emit(&streamAuthorityFailure{reason: "SECRET_TOKEN"}, false)
	o.emit(&streamAuthorityFailure{reason: "renewal_query_failed", elapsed: -1}, false)
	o.emit(&streamAuthorityFailure{reason: "deadline_set_failed"}, true)
	o.emit(&streamAuthorityFailure{reason: "renewal_query_failed"}, false)
	o.emit(&streamAuthorityFailure{reason: "context_done"}, false)
	reasons := authorityReasons(t, b)
	if len(reasons) != 2 || strings.Contains(b.String(), "SECRET_TOKEN") {
		t.Fatal("diagnostic vocabulary/count privacy")
	}
}

type authorityPanicHandler struct{}

func (authorityPanicHandler) Enabled(context.Context, slog.Level) bool { return true }
func (authorityPanicHandler) Handle(context.Context, slog.Record) error {
	panic("SECRET_HANDLER_ERROR")
}
func (h authorityPanicHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h authorityPanicHandler) WithGroup(string) slog.Handler      { return h }
func TestStreamAuthorityObservationBrokenLoggerPreservesFailure(t *testing.T) {
	old := slog.Default()
	slog.SetDefault(slog.New(authorityPanicHandler{}))
	defer slog.SetDefault(old)
	a, err := newStreamAuthority(context.Background(), func(context.Context) (time.Time, error) { return time.Time{}, io.ErrUnexpectedEOF }, func(time.Time) error { return nil }, nil)
	if a != nil || !errors.Is(err, errStreamAuthority) {
		t.Fatal("broken observer replaced original failure")
	}
}
func TestStreamAuthorityObservationNormalStopDuringRenewalSilent(t *testing.T) {
	b := authorityLog(t)
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{})
	a := &streamAuthority{ctx: ctx, cancel: cancel, until: time.Now().Add(1100 * time.Millisecond), resolve: func(ctx context.Context) (time.Time, error) {
		close(entered)
		<-ctx.Done()
		return time.Time{}, ctx.Err()
	}, setDeadline: func(time.Time) error { return nil }, done: make(chan struct{})}
	go a.watch()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("renewal not entered")
	}
	a.stop()
	if b.Len() != 0 {
		t.Fatal("normal in-flight stop emitted authority failure")
	}
}
func TestStreamAuthorityObservationAbortSetterFailureRecordedAfterCancellation(t *testing.T) {
	b := authorityLog(t)
	ctx, cancel := context.WithCancel(context.Background())
	a := &streamAuthority{ctx: ctx, cancel: cancel, until: time.Now().Add(time.Minute), setDeadline: func(time.Time) error {
		if ctx.Err() == nil {
			t.Error("deadline interruption preceded cancellation")
		}
		return io.ErrClosedPipe
	}}
	if !a.abort() || a.valid() {
		t.Fatal("abort semantics changed")
	}
	reasons := authorityReasons(t, b)
	if len(reasons) != 1 || reasons[0] != "abort_deadline_set_failed" {
		t.Fatal("abort setter failure unobserved")
	}
}
