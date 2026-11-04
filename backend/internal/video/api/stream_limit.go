package videoapi

import (
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/domain"
)

// FN-G-14 values (docs/video/13-data-structures.md): concurrent streams and the speed of one stream.
const (
	streamsPerSession  = 2
	streamsPerShare    = 4
	streamBytesPerSec  = 4 << 20
	streamBurstBytes   = 8 << 20
	streamLimitCode    = "VIDEO_STREAM_LIMIT"
	streamLimitStatus  = 429
	streamPaceMinSleep = 5 * time.Millisecond
)

// streamLimiter counts open responses per portal session and per share. It lives in the process, so with several portal
// instances the limits apply per instance (the spec's proxy mode accepts this; redirect mode has its own limits).
type streamLimiter struct {
	mu      sync.Mutex
	session map[uuid.UUID]int
	share   map[uuid.UUID]int
}

func (l *streamLimiter) acquire(session, share uuid.UUID) (release func(), err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.session == nil {
		l.session, l.share = map[uuid.UUID]int{}, map[uuid.UUID]int{}
	}
	if l.session[session] >= streamsPerSession || l.share[share] >= streamsPerShare {
		return nil, domain.NewError(streamLimitCode, streamLimitStatus)
	}
	l.session[session]++
	l.share[share]++
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			dec(l.session, session)
			dec(l.share, share)
		})
	}, nil
}

func dec(m map[uuid.UUID]int, k uuid.UUID) {
	if m[k]--; m[k] <= 0 {
		delete(m, k)
	}
}

// pacedWriter limits one response to streamBytesPerSec after an initial burst of streamBurstBytes.
type pacedWriter struct {
	http.ResponseWriter
	start time.Time
	sent  int64
	rate  int64
	burst int64
}

func newPaced(w http.ResponseWriter) *pacedWriter {
	return &pacedWriter{ResponseWriter: w, start: time.Now(), rate: streamBytesPerSec, burst: streamBurstBytes}
}

func (p *pacedWriter) Write(b []byte) (int, error) {
	n, err := p.ResponseWriter.Write(b)
	p.sent += int64(n)
	// bytes beyond the burst may only be sent at the allowed rate since the start
	if over := p.sent - p.burst; over > 0 {
		due := p.start.Add(time.Duration(float64(over) / float64(p.rate) * float64(time.Second)))
		if d := time.Until(due); d > streamPaceMinSleep {
			time.Sleep(d)
		}
	}
	return n, err
}

func (p *pacedWriter) Unwrap() http.ResponseWriter { return p.ResponseWriter }

// open is how many streams the share has open in this process.
func (l *streamLimiter) open(share uuid.UUID) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.share[share]
}
