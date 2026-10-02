package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"sync/atomic"
	"testing"
	"time"
)

type ingestProcessorLease struct {
	renew           func(int64) (bool, error)
	calls, releases atomic.Int64
	empty           bool
}

func (l *ingestProcessorLease) Claim(context.Context) (*IngestBuildLease, error) {
	if l.empty {
		return nil, nil
	}
	return &IngestBuildLease{TenantID: uuid.New(), RunID: uuid.New()}, nil
}
func (l *ingestProcessorLease) Renew(context.Context, IngestBuildLease) (bool, error) {
	return l.renew(l.calls.Add(1))
}
func (l *ingestProcessorLease) Release(context.Context, IngestBuildLease) (bool, error) {
	l.releases.Add(1)
	return true, nil
}

type ingestAdvanceFunc func(context.Context, uuid.UUID, uuid.UUID) (bool, error)

func (f ingestAdvanceFunc) Advance(c context.Context, t, r uuid.UUID) (bool, error) {
	return f(c, t, r)
}

func TestIngestProcessorLosesLeaseAndReleases(t *testing.T) {
	for _, initial := range []bool{true, false} {
		t.Run(map[bool]string{true: "heartbeat", false: "initial"}[initial], func(t *testing.T) {
			l := &ingestProcessorLease{renew: func(n int64) (bool, error) { return initial && n == 1, nil }}
			called := false
			p := IngestProcessor{Leases: l, Runner: ingestAdvanceFunc(func(c context.Context, tenant, run uuid.UUID) (bool, error) {
				called = true
				<-c.Done()
				return false, c.Err()
			}), HeartbeatInterval: time.Millisecond, WorkTimeout: time.Second}
			changed, err := p.ProcessOne(context.Background())
			if changed || !errors.Is(err, ErrIngestLeaseLost) || called != initial || l.releases.Load() != 1 {
				t.Fatal("lease loss not respected", err)
			}
		})
	}
}
func TestIngestProcessorIdleSuccessAndBuildError(t *testing.T) {
	for _, mode := range []string{"idle", "success", "error"} {
		t.Run(mode, func(t *testing.T) {
			l := &ingestProcessorLease{empty: mode == "idle", renew: func(int64) (bool, error) { return true, nil }}
			called := false
			failure := errors.New("build failed")
			p := IngestProcessor{Leases: l, Runner: ingestAdvanceFunc(func(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
				called = true
				if mode == "error" {
					return false, failure
				}
				return true, nil
			})}
			changed, err := p.ProcessOne(context.Background())
			if mode == "idle" {
				if called || changed || err != nil || l.releases.Load() != 0 {
					t.Fatal("idle changed state")
				}
				return
			}
			if !called || l.releases.Load() != 1 {
				t.Fatal("lease leaked")
			}
			if mode == "success" && (!changed || err != nil) {
				t.Fatal("success lost", err)
			}
			if mode == "error" && (changed || !errors.Is(err, failure)) {
				t.Fatal("build error lost", err)
			}
		})
	}
}
func TestSQLIngestLeaseTokenBinding(t *testing.T) {
	tenant, run, token := uuid.New(), uuid.New(), uuid.New()
	until := time.Now().Add(time.Minute)
	db := &expansionQueries{row: expansionRow{values: []any{tenant, run, token, until}}}
	s := SQLIngestLeases{DB: db}
	lease, err := s.Claim(context.Background())
	if err != nil || lease.RunID != run || lease.Token != token {
		t.Fatal("claim failed", err)
	}
	db.row = expansionRow{values: []any{true}}
	for _, update := range []func(context.Context, IngestBuildLease) (bool, error){s.Renew, s.Release} {
		ok, err := update(context.Background(), *lease)
		if err != nil || !ok || len(db.args) != 2 || db.args[0] != run || db.args[1] != token {
			t.Fatal("lease token not bound", err)
		}
	}
	db.row = expansionRow{err: pgx.ErrNoRows}
	lease, err = s.Claim(context.Background())
	if lease != nil || err != nil {
		t.Fatal("empty queue error", err)
	}
}
