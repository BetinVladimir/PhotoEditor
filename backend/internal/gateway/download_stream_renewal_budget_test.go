package gateway

import (
	"context"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// A healthy live DB check can take longer than one second. Starting it in
// the last second of a five-second authority window truncates a valid stream.
// The check must still finish before the previous window expires.
func TestStreamAuthorityHealthySlowRenewalKeepsBlockedWriteAlive(t *testing.T) {
	var checks atomic.Int32
	w := &deadlineStreamWriter{ResponseRecorder: httptest.NewRecorder(), delay: 6200 * time.Millisecond}
	a, err := newStreamAuthority(context.Background(), func(ctx context.Context) (time.Time, error) {
		if checks.Add(1) > 1 {
			timer := time.NewTimer(1200 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return time.Time{}, ctx.Err()
			case <-timer.C:
			}
		}
		return time.Now().Add(time.Minute), nil
	}, w.SetWriteDeadline, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.stop()
	n, err := w.Write([]byte("healthy"))
	if err != nil || n != 7 || checks.Load() < 2 {
		t.Fatalf("healthy renewal exhausted previous authority: bytes=%d checks=%d err=%v", n, checks.Load(), err)
	}
}
