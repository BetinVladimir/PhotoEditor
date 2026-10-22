package gateway

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestStreamAuthorityBlockedHealthyWriteRenewsBeyondFiveSeconds(t *testing.T) {
	var checks atomic.Int32
	w := &deadlineStreamWriter{ResponseRecorder: httptest.NewRecorder(), delay: 6200 * time.Millisecond}
	a, e := newStreamAuthority(context.Background(), func(context.Context) (time.Time, error) { checks.Add(1); return time.Now().Add(time.Minute), nil }, w.SetWriteDeadline, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer a.stop()
	n, e := w.Write([]byte("healthy"))
	if e != nil || n != 7 || checks.Load() < 2 {
		t.Fatalf("healthy blocked write truncated: n=%d checks=%d", n, checks.Load())
	}
}
func TestStreamAuthorityCancellationInterruptsWriteAndJoins(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &deadlineStreamWriter{ResponseRecorder: httptest.NewRecorder(), delay: time.Second}
	a, e := newStreamAuthority(ctx, func(context.Context) (time.Time, error) { return time.Now().Add(time.Minute), nil }, w.SetWriteDeadline, nil)
	if e != nil {
		t.Fatal(e)
	}
	time.AfterFunc(30*time.Millisecond, cancel)
	start := time.Now()
	_, e = w.Write([]byte("private"))
	a.stop()
	if !errors.Is(e, os.ErrDeadlineExceeded) || time.Since(start) > 500*time.Millisecond || w.Body.Len() != 0 || a.valid() {
		t.Fatal("cancel did not interrupt write")
	}
}
func TestStreamAuthorityFixedLeaseExpiresWithoutGrace(t *testing.T) {
	lease := time.Now().Add(80 * time.Millisecond)
	w := &deadlineStreamWriter{ResponseRecorder: httptest.NewRecorder(), delay: time.Second}
	a, e := newStreamAuthority(context.Background(), func(context.Context) (time.Time, error) { return lease, nil }, w.SetWriteDeadline, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer a.stop()
	_, e = w.Write([]byte("private"))
	if !errors.Is(e, os.ErrDeadlineExceeded) || w.Body.Len() != 0 || a.valid() {
		t.Fatal("lease expiry admitted write")
	}
}
func TestStreamAuthorityLateGuardCannotReviveExpiredWindow(t *testing.T) {
	var count atomic.Int32
	w := &deadlineStreamWriter{ResponseRecorder: httptest.NewRecorder()}
	a, e := newStreamAuthority(context.Background(), func(ctx context.Context) (time.Time, error) {
		if count.Add(1) > 1 {
			time.Sleep(100 * time.Millisecond)
			return time.Now().Add(time.Minute), nil
		}
		return time.Now().Add(40 * time.Millisecond), nil
	}, w.SetWriteDeadline, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer a.stop()
	if e = a.refresh(); e == nil || a.valid() {
		t.Fatal("late guard revived authority")
	}
}
func TestStreamAuthorityRefreshRevocationFailsClosed(t *testing.T) {
	var count atomic.Int32
	w := &deadlineStreamWriter{ResponseRecorder: httptest.NewRecorder()}
	a, e := newStreamAuthority(context.Background(), func(context.Context) (time.Time, error) {
		if count.Add(1) > 1 {
			return time.Time{}, io.ErrUnexpectedEOF
		}
		return time.Now().Add(time.Minute), nil
	}, w.SetWriteDeadline, nil)
	if e != nil {
		t.Fatal(e)
	}
	// Exercise the same failed renewal branch as the watcher without a 4s wait.
	if a.refresh() == nil {
		t.Fatal("revocation admitted")
	}
	a.abort()
	a.stop()
	if a.valid() {
		t.Fatal("revocation remained valid")
	}
}

func TestStreamAuthorityRevocationCancelsActualStorageContext(t *testing.T) {
	storageCtx, storageCancel := context.WithCancel(context.Background())
	defer storageCancel()
	var calls atomic.Int32
	a, err := newStreamAuthority(storageCtx, func(context.Context) (time.Time, error) {
		if calls.Add(1) > 1 {
			return time.Time{}, errStreamAuthority
		}
		return time.Now().Add(time.Minute), nil
	}, func(time.Time) error { return nil }, storageCancel)
	if err != nil {
		t.Fatal(err)
	}
	readFinished := make(chan struct{})
	go func() { <-storageCtx.Done(); close(readFinished) }()
	if a.refresh() == nil {
		t.Fatal("revocation admitted")
	}
	a.abort()
	select {
	case <-readFinished:
	case <-time.After(time.Second):
		t.Fatal("actual storage context remained live")
	}
	a.stop()
}
func TestStreamAuthorityNormalStopDoesNotCancelStorageOrSetDeadline(t *testing.T) {
	storageCtx, storageCancel := context.WithCancel(context.Background())
	defer storageCancel()
	var sets atomic.Int32
	var aborts atomic.Int32
	a, err := newStreamAuthority(storageCtx, func(context.Context) (time.Time, error) { return time.Now().Add(time.Minute), nil }, func(time.Time) error { sets.Add(1); return nil }, func() { aborts.Add(1); storageCancel() })
	if err != nil {
		t.Fatal(err)
	}
	before := sets.Load()
	a.stop()
	if sets.Load() != before || aborts.Load() != 0 || storageCtx.Err() != nil {
		t.Fatal("normal stop mutated stream authority/deadline")
	}
	// stop joined watcher: no deferred stale mutation is possible.
}

func TestStreamAuthorityWatcherRevocationInterruptsBlockedWrite(t *testing.T) {
	var calls atomic.Int32
	w := &deadlineStreamWriter{ResponseRecorder: httptest.NewRecorder(), delay: 2 * time.Second}
	a, err := newStreamAuthority(context.Background(), func(context.Context) (time.Time, error) {
		if calls.Add(1) > 1 {
			return time.Time{}, errStreamAuthority
		}
		return time.Now().Add(1200 * time.Millisecond), nil
	}, w.SetWriteDeadline, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.stop()
	start := time.Now()
	_, err = w.Write([]byte("private"))
	if !errors.Is(err, os.ErrDeadlineExceeded) || calls.Load() != 2 || time.Since(start) > time.Second || w.Body.Len() != 0 || a.valid() {
		t.Fatal("watcher revocation did not interrupt blocked write")
	}
}
