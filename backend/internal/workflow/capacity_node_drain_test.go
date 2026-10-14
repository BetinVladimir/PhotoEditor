package workflow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type drainRow struct {
	values []any
	err    error
}

func (r drainRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.values) {
		return errors.New("scan arity")
	}
	for i, value := range r.values {
		switch p := dest[i].(type) {
		case *int64:
			*p = value.(int64)
		case *int:
			*p = value.(int)
		case *bool:
			*p = value.(bool)
		default:
			return errors.New("unexpected scan type")
		}
	}
	return nil
}

func TestSQLNodeDrainerReapExpiredCount(t *testing.T) {
	q := &drainQueries{rows: []drainRow{
		{values: []any{1}},
		{values: []any{0}},
		{values: []any{2}},
	}}
	d := SQLNodeDrainer{DB: q}
	if reaped, err := d.ReapExpired(context.Background(), "gpu-a", 7); err != nil || !reaped {
		t.Fatal(reaped, err)
	}
	if reaped, err := d.ReapExpired(context.Background(), "gpu-a", 7); err != nil || reaped {
		t.Fatal(reaped, err)
	}
	if _, err := d.ReapExpired(context.Background(), "gpu-a", 7); err == nil {
		t.Fatal("invalid reap count accepted")
	}
}

func TestSQLNodeDrainerWaitReapsExpiredTaskBeforeReady(t *testing.T) {
	q := &drainQueries{rows: []drainRow{
		{values: []any{int64(1), int64(1), false}},
		{values: []any{1}},
		{values: []any{int64(0), int64(0), true}},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	status, err := (SQLNodeDrainer{DB: q}).WaitReady(ctx, "gpu-a", 7, time.Millisecond)
	if err != nil || !status.ReadyToStop || q.calls != 3 {
		t.Fatal(status, err, q.calls)
	}
}

type drainQueries struct {
	rows  []drainRow
	calls int
}

type recordingNodeStopper struct {
	calls      int
	node       string
	generation int64
}

func (s *recordingNodeStopper) StopNode(_ context.Context, node string, generation int64) error {
	s.calls++
	s.node, s.generation = node, generation
	return nil
}

func (q *drainQueries) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	row := q.rows[q.calls]
	q.calls++
	return row
}

func TestSQLNodeDrainerGenerationAndReadiness(t *testing.T) {
	q := &drainQueries{rows: []drainRow{
		{values: []any{int64(7)}},
		{values: []any{int64(1), int64(1), false}},
		{values: []any{int64(0), int64(0), true}},
	}}
	d := SQLNodeDrainer{DB: q}
	gen, err := d.Begin(context.Background(), "gpu-a")
	if err != nil || gen != 7 {
		t.Fatal(gen, err)
	}
	status, err := d.Status(context.Background(), "gpu-a", gen)
	if err != nil || status.ReadyToStop || status.ActiveReservations != 1 {
		t.Fatal(status, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	status, err = d.WaitReady(ctx, "gpu-a", gen, time.Nanosecond)
	if err != nil || !status.ReadyToStop || status.Generation != gen {
		t.Fatal(status, err)
	}
}

func TestSQLNodeDrainerRejectsInvalidProofAndContradiction(t *testing.T) {
	q := &drainQueries{rows: []drainRow{{values: []any{int64(1), int64(0), true}}}}
	d := SQLNodeDrainer{DB: q}
	if _, err := d.Status(context.Background(), "gpu-a", 0); err == nil || q.calls != 0 {
		t.Fatal("invalid proof reached database")
	}
	if _, err := d.Status(context.Background(), "gpu-a", 1); err == nil {
		t.Fatal("contradictory status accepted")
	}
	if _, err := d.WaitReady(context.Background(), "gpu-a", 1, 31*time.Second); err == nil {
		t.Fatal("unbounded poll accepted")
	}
}

func TestStopReadyNodeRequiresFreshReadyProof(t *testing.T) {
	q := &drainQueries{rows: []drainRow{
		{values: []any{int64(1), int64(0), false}},
		{values: []any{int64(0), int64(0), true}},
	}}
	d := SQLNodeDrainer{DB: q}
	stopper := &recordingNodeStopper{}
	if err := d.StopReadyNode(context.Background(), "gpu-a", 7, nil); err == nil || q.calls != 0 {
		t.Fatal("absent provisioner reached database")
	}
	if err := d.StopReadyNode(context.Background(), "gpu-a", 7, stopper); err == nil || stopper.calls != 0 {
		t.Fatal("active node stopped")
	}
	if err := d.StopReadyNode(context.Background(), "gpu-a", 7, stopper); err != nil ||
		stopper.calls != 1 || stopper.node != "gpu-a" || stopper.generation != 7 {
		t.Fatal("ready node was not stopped with pinned generation", err)
	}
}
