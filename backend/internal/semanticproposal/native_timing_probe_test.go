package semanticproposal

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Test-only profiling never retains SQL arguments, packets, DSNs or credentials.
// Hashes group exact query shapes; duration includes the real database wait.
type nativeTimingTracer struct {
	mu   sync.Mutex
	rows map[string]nativeQueryTiming
}
type nativeQueryTiming struct {
	count      int
	total, max time.Duration
}
type nativeTimingKey struct{}
type nativeTimingStart struct {
	at  time.Time
	key string
}

func newNativeTimingTracer(t *testing.T) *nativeTimingTracer {
	tracer := &nativeTimingTracer{rows: make(map[string]nativeQueryTiming)}
	t.Cleanup(func() {
		tracer.mu.Lock()
		defer tracer.mu.Unlock()
		keys := make([]string, 0, len(tracer.rows))
		for key := range tracer.rows {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool { return tracer.rows[keys[i]].total > tracer.rows[keys[j]].total })
		for _, key := range keys {
			row := tracer.rows[key]
			t.Logf("NATIVE_SQL_TIMING shape=%s count=%d total=%s max=%s", key, row.count, row.total, row.max)
		}
	})
	return tracer
}
func (t *nativeTimingTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	digest := sha256.Sum256([]byte(data.SQL))
	return context.WithValue(ctx, nativeTimingKey{}, nativeTimingStart{time.Now(), fmt.Sprintf("%x", digest[:8])})
}
func (t *nativeTimingTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	start, ok := ctx.Value(nativeTimingKey{}).(nativeTimingStart)
	if !ok {
		return
	}
	elapsed := time.Since(start.at)
	t.mu.Lock()
	defer t.mu.Unlock()
	row := t.rows[start.key]
	row.count++
	row.total += elapsed
	if elapsed > row.max {
		row.max = elapsed
	}
	t.rows[start.key] = row
}

func TestNativeTimingGroupsShapeWithoutArguments(t *testing.T) {
	tracer := newNativeTimingTracer(t)
	for _, secret := range []string{"secret-a", "secret-b"} {
		ctx := tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT photo.authorize_tenant($1,$2,$3)", Args: []any{secret}})
		tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
	}
	if len(tracer.rows) != 1 {
		t.Fatal("query grouping depends on arguments")
	}
	for key, row := range tracer.rows {
		if len(key) != 16 || row.count != 2 || row.total < row.max {
			t.Fatal("invalid timing aggregation")
		}
	}
}
