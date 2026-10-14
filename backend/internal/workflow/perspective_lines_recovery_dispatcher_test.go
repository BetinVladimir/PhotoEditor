package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type linesRecoveryQueue struct {
	items   []*PerspectiveLinesRecoveryCandidate
	cursors []uuid.UUID
}

func (q *linesRecoveryQueue) NextPerspectiveLinesRecovery(_ context.Context, after uuid.UUID) (*PerspectiveLinesRecoveryCandidate, error) {
	q.cursors = append(q.cursors, after)
	if len(q.items) == 0 {
		return nil, nil
	}
	c := q.items[0]
	q.items = q.items[1:]
	return c, nil
}

type linesRecoveryResumer struct {
	calls   []PerspectiveLinesRecoveryCandidate
	fail    error
	bounded bool
}

func (r *linesRecoveryResumer) ResumeSealed(ctx context.Context, tenant, run uuid.UUID) error {
	r.calls = append(r.calls, PerspectiveLinesRecoveryCandidate{Tenant: tenant, Run: run})
	_, r.bounded = ctx.Deadline()
	err := r.fail
	r.fail = nil
	return err
}
func TestPerspectiveLinesRecoveryDispatcherAdvancesFailedCandidateAndWraps(t *testing.T) {
	first := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	second := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	tenant := uuid.New()
	q := &linesRecoveryQueue{items: []*PerspectiveLinesRecoveryCandidate{{Tenant: tenant, Run: first}, {Tenant: tenant, Run: second}, nil, {Tenant: tenant, Run: first}}}
	transient := errors.New("source changed")
	r := &linesRecoveryResumer{fail: transient}
	d := &PerspectiveLinesRecoveryDispatcher{Candidates: q, Resumer: r}
	if ok, err := d.ProcessOne(context.Background()); ok || !errors.Is(err, transient) {
		t.Fatal(ok, err)
	}
	if ok, err := d.ProcessOne(context.Background()); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if ok, err := d.ProcessOne(context.Background()); ok || err != nil {
		t.Fatal(ok, err)
	}
	if ok, err := d.ProcessOne(context.Background()); !ok || err != nil {
		t.Fatal(ok, err)
	}
	expected := []uuid.UUID{uuid.Nil, first, second, uuid.Nil}
	for i, c := range q.cursors {
		if c != expected[i] {
			t.Fatal("cursor did not advance/wrap", q.cursors)
		}
	}
	if len(r.calls) != 3 || !r.bounded {
		t.Fatal("missing bounded recovery")
	}
}
func TestPerspectiveLinesRecoveryDispatcherRejectsInvalidOrCancelledWork(t *testing.T) {
	q := &linesRecoveryQueue{}
	r := &linesRecoveryResumer{}
	d := &PerspectiveLinesRecoveryDispatcher{Candidates: q, Resumer: r, Timeout: 6 * time.Minute}
	if _, err := d.ProcessOne(context.Background()); err == nil {
		t.Fatal("unbounded timeout accepted")
	}
	d.Timeout = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.ProcessOne(ctx); err != context.Canceled || len(q.cursors) != 0 {
		t.Fatal("cancelled query executed", err)
	}
	q.items = []*PerspectiveLinesRecoveryCandidate{{Tenant: uuid.Nil, Run: uuid.New()}}
	if _, err := d.ProcessOne(context.Background()); err == nil || len(r.calls) != 0 {
		t.Fatal("invalid scope resumed", err)
	}
}
