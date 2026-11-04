package videoapi

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestStreamLimiterPerSessionAndShare(t *testing.T) {
	var l streamLimiter
	share, s1, s2, s3 := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	var rel []func()
	for i := 0; i < streamsPerSession; i++ {
		r, err := l.acquire(s1, share)
		if err != nil {
			t.Fatalf("stream %d: %v", i, err)
		}
		rel = append(rel, r)
	}
	if _, err := l.acquire(s1, share); err == nil {
		t.Fatal("third stream of one session must be refused")
	}
	for i := 0; i < streamsPerShare-streamsPerSession; i++ {
		r, err := l.acquire(s2, share)
		if err != nil {
			t.Fatalf("second session stream %d: %v", i, err)
		}
		rel = append(rel, r)
	}
	if _, err := l.acquire(s3, share); err == nil {
		t.Fatal("share cap must hold across sessions")
	}
	rel[0]()
	rel[0]() // release is idempotent
	if r, err := l.acquire(s3, share); err != nil {
		t.Fatalf("a freed slot must be reusable: %v", err)
	} else {
		r()
	}
	for _, r := range rel {
		r()
	}
	if len(l.session) != 0 || len(l.share) != 0 {
		t.Fatalf("counters not cleaned: %v %v", l.session, l.share)
	}
}

func TestPacedWriterThrottlesAfterBurst(t *testing.T) {
	rec := httptest.NewRecorder()
	p := newPaced(rec)
	p.rate, p.burst = 1000, 100 // 1000 B/s after 100 B burst
	start := time.Now()
	if _, err := p.Write(make([]byte, 100)); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatal("the burst must not be delayed")
	}
	if _, err := p.Write(make([]byte, 200)); err != nil { // 200 B over the burst = 0.2 s
		t.Fatal(err)
	}
	if d := time.Since(start); d < 150*time.Millisecond {
		t.Fatalf("200 bytes past the burst took %v, want about 200ms", d)
	}
	if rec.Body.Len() != 300 {
		t.Fatalf("body %d", rec.Body.Len())
	}
}
