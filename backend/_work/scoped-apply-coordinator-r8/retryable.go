package main

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/workflow"
	"time"
)

var retryableRun = uuid.MustParse("ead9427d-dc06-45d1-8d66-649cc0e1ce33")
var retryableTask = uuid.MustParse("e3bb300a-cb2a-5823-91dd-ed14eae2b91d")

func retryableScope(s scope, mode string) error {
	if s.Run != retryableRun || s.Phase != "quality" || len(s.Tasks) != 9 || (mode != "recover_retryable_plan" && mode != "recover_retryable_activate") {
		return errors.New("exact retryable recovery scope")
	}
	found := false
	for _, t := range s.Tasks {
		if t.ID == retryableTask {
			if t.Operation != "image.local" || t.Class != "cpu" {
				return errors.New("retryable task operation")
			}
			found = true
		}
	}
	if !found {
		return errors.New("retryable task missing")
	}
	return nil
}
func retryableBefore(ctx context.Context, tx pgx.Tx, s scope, mode string) error {
	if e := retryableScope(s, mode); e != nil {
		return e
	}
	var ok bool
	e := tx.QueryRow(ctx, `SELECT (SELECT count(*)FROM photo.tasks WHERE state IN('queued','running'))=0 AND (SELECT count(*)FROM photo.tasks WHERE state='retry_wait')=1 AND EXISTS(SELECT FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id JOIN photo.apply_builds b ON b.tenant_id=r.tenant_id AND b.run_id=r.id JOIN photo.task_attempts a ON a.tenant_id=t.tenant_id AND a.task_id=t.id AND a.attempt=t.attempt AND a.fencing_token=t.fencing_token AND a.worker_id=t.worker_id WHERE t.id=$1 AND t.run_id=$2 AND t.tenant_id=$3 AND t.operation='image.local' AND t.resource_class='cpu' AND t.state='retry_wait' AND t.attempt=1 AND t.fencing_token=1 AND t.canonical_result_id IS NULL AND t.oom_count=0 AND NOT t.cancel_requested AND NOT r.cancel_requested AND r.graph_sealed AND r.status IN('queued','running') AND b.phase='quality' AND b.stage_index=3 AND a.finished_at IS NOT NULL AND a.error->>'code'='DEPENDENCY_UNAVAILABLE' AND a.error->'retryable'='true'::jsonb AND t.terminal_reason='DEPENDENCY_UNAVAILABLE' AND (($4='recover_retryable_plan' AND t.retry_count=0 AND t.next_attempt_at IS NULL AND EXISTS(SELECT FROM photo.pending_task_failures(2)p WHERE p.task_id=t.id AND p.attempt=1 AND p.fence=1 AND NOT p.cancel_requested AND p.error=a.error)) OR ($4='recover_retryable_activate' AND t.retry_count=1 AND t.next_attempt_at IS NOT NULL AND t.next_attempt_at<=clock_timestamp()))) AND (SELECT count(*)FROM photo.tasks WHERE run_id=$2 AND tenant_id=$3 AND state='succeeded')=6 AND (SELECT count(*)FROM photo.tasks WHERE run_id=$2 AND tenant_id=$3 AND state='ready' AND operation='image.local')=2`, retryableTask, s.Run, s.Tenant, mode).Scan(&ok)
	if e != nil {
		return e
	}
	if !ok {
		return errors.New("retryable recovery before predicate")
	}
	return nil
}
func retryableAfter(ctx context.Context, tx pgx.Tx, s scope, mode string, count int) error {
	if count != 1 {
		return errors.New("exactly one recovery mutation required")
	}
	var ok bool
	e := tx.QueryRow(ctx, `SELECT attempt=1 AND fencing_token=1 AND canonical_result_id IS NULL AND retry_count=1 AND oom_count=0 AND (($4='recover_retryable_plan' AND state='retry_wait' AND next_attempt_at>clock_timestamp() AND terminal_reason='DEPENDENCY_UNAVAILABLE') OR ($4='recover_retryable_activate' AND state='ready' AND next_attempt_at IS NULL AND dispatch_command IS NULL AND terminal_reason IS NULL)) FROM photo.tasks WHERE id=$1 AND run_id=$2 AND tenant_id=$3`, retryableTask, s.Run, s.Tenant, mode).Scan(&ok)
	if e != nil {
		return e
	}
	if !ok {
		return errors.New("retryable recovery exact postcondition")
	}
	return nil
}
func retryableComponentOne(ctx context.Context, p *pgxpool.Pool, s scope, mode string) (int, error) {
	if e := s.validate(time.Now().UTC()); e != nil {
		return 0, e
	}
	if e := retryableScope(s, mode); e != nil {
		return 0, e
	}
	s.retryMode = mode
	tx, e := p.Begin(ctx)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback(context.Background())
	if e = observedTasks(ctx, tx, s); e != nil {
		return 0, e
	}
	if e = retryableBefore(ctx, tx, s, mode); e != nil {
		return 0, e
	}
	before, e := capture(ctx, tx, s)
	if e != nil {
		return 0, e
	}
	if _, e = tx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); e != nil {
		return 0, e
	}
	n, e := (workflow.Recovery{DB: tx}).Recover(ctx, 1)
	if e != nil {
		return 0, e
	}
	if _, e = tx.Exec(ctx, "RESET ROLE"); e != nil {
		return 0, e
	}
	if e = retryableAfter(ctx, tx, s, mode, n); e != nil {
		return 0, e
	}
	if e = observedTasks(ctx, tx, s); e != nil {
		return 0, e
	}
	after, e := capture(ctx, tx, s)
	if e != nil {
		return 0, e
	}
	if !equalSnapshots(before, after) {
		return 0, errors.New("retryable recovery protected history changed")
	}
	if e = tx.Commit(ctx); e != nil {
		return 0, e
	}
	return n, nil
}
