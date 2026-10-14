package semanticproposal

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Diagnostic aggregates only. SQL text, arguments, principals and connection
// strings must never be printed by this tracer.
type integrationQueryTiming struct {
	mu     sync.Mutex
	groups map[string]integrationQueryTimingGroup
}
type integrationQueryTimingGroup struct {
	Count      int
	Total, Max time.Duration
}
type integrationQueryTimingKey struct{}
type integrationQueryTimingStart struct {
	At    time.Time
	Group string
}

func (q *integrationQueryTiming) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	group := "catalog"
	text := strings.ToLower(strings.TrimSpace(data.SQL))
	switch {
	case strings.HasPrefix(text, "begin"), strings.HasPrefix(text, "commit"), strings.HasPrefix(text, "rollback"):
		group = "transaction"
	case strings.Contains(text, "authorize_tenant"):
		group = "tenant_authorization"
	case strings.Contains(text, "semantic_proposal_defect_origin"):
		group = "defect_origin"
	case strings.Contains(text, "semantic_defect_saved_config"):
		group = "saved_config"
	case strings.Contains(text, "qualification"):
		group = "qualification"
	}
	return context.WithValue(ctx, integrationQueryTimingKey{}, integrationQueryTimingStart{At: time.Now(), Group: group})
}
func (q *integrationQueryTiming) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	start, ok := ctx.Value(integrationQueryTimingKey{}).(integrationQueryTimingStart)
	if !ok {
		return
	}
	duration := time.Since(start.At)
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.groups == nil {
		q.groups = map[string]integrationQueryTimingGroup{}
	}
	g := q.groups[start.Group]
	g.Count++
	g.Total += duration
	if duration > g.Max {
		g.Max = duration
	}
	q.groups[start.Group] = g
}
func (q *integrationQueryTiming) log(t *testing.T) {
	t.Helper()
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, name := range []string{"transaction", "tenant_authorization", "defect_origin", "saved_config", "qualification", "catalog"} {
		g := q.groups[name]
		t.Logf("V27_SQL_TIMING group=%s count=%d total_seconds=%.3f max_seconds=%.3f", name, g.Count, g.Total.Seconds(), g.Max.Seconds())
	}
}
