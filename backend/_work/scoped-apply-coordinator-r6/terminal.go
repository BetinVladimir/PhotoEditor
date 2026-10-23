package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"os"
	"photoeditor/backend/internal/workflow"
)

const main2655ReceiptSHA = "275a0cd4db590d8d37bea9c9d6cab10794438baa60f5076322285e62fca993f5"

var oldApplyRun = uuid.MustParse("7a6d6640-404f-417a-98cd-fe3341ef6ef8")
var oldFailedTask = uuid.MustParse("41fbebef-5c6a-5539-b64e-139981c63393")

func checkedMain2655() error {
	raw, e := os.ReadFile("/Users/freelancer/Documents/PhotoEditor/work/apply-main-2655-r4/root-main2655-installed.json")
	if e != nil {
		return errors.New("MAIN2655 receipt unavailable")
	}
	h := sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != main2655ReceiptSHA {
		return errors.New("MAIN2655 receipt drift")
	}
	var p struct {
		Status    string `json:"status"`
		Head      int    `json:"head"`
		Exit      int    `json:"normal_goose_exit"`
		Startup   bool   `json:"restricted_login_startup_pass"`
		Migration string `json:"migration_sha256"`
	}
	if json.Unmarshal(raw, &p) != nil || p.Status != "ROOT_REVIEWED_MAIN2655_INSTALLED" || p.Head != 2655 || p.Exit != 0 || !p.Startup || p.Migration != "fc3e6fffd7b53694d93e0d31485f08ab79be46b14e614e85157a78fc43d7cd25" {
		return errors.New("MAIN2655 receipt semantics")
	}
	return nil
}
func oldRecoveryReady(ctx context.Context, tx pgx.Tx, s scope) error {
	if s.Run != oldApplyRun || s.Phase != "quality" || len(s.Tasks) != 9 {
		return errors.New("exact old recovery scope")
	}
	var ok bool
	e := tx.QueryRow(ctx, `SELECT (SELECT count(*)FROM photo.tasks WHERE state IN('retry_wait','queued','running'))=1 AND EXISTS(SELECT FROM photo.pending_task_failures(2)p JOIN photo.tasks t ON t.id=p.task_id WHERE p.task_id=$1 AND t.run_id=$2 AND t.tenant_id=$3 AND t.operation='image.local' AND t.state='retry_wait' AND p.error->>'code'='INTERNAL_ERROR' AND p.error->'retryable'='false'::jsonb AND NOT p.cancel_requested)`, oldFailedTask, s.Run, s.Tenant).Scan(&ok)
	if e != nil {
		return e
	}
	if !ok {
		return errors.New("global recovery eligible scope drift")
	}
	return nil
}
func oldFailureReady(ctx context.Context, tx pgx.Tx, s scope) error {
	if s.Run != oldApplyRun || s.Phase != "quality" || len(s.Tasks) != 9 {
		return errors.New("exact old failure scope")
	}
	if e := observedTasks(ctx, tx, s); e != nil {
		return e
	}
	var ok bool
	e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT FROM photo.apply_builds b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id JOIN photo.apply_v4_stages st ON st.tenant_id=b.tenant_id AND st.run_id=b.run_id AND st.stage_index=b.stage_index-1 WHERE b.tenant_id=$1 AND b.run_id=$2 AND b.phase='quality' AND b.stage_index=3 AND st.phase='local' AND st.operation='image.local' AND st.cardinality='item' AND b.item_count=3 AND r.graph_sealed AND NOT r.cancel_requested AND r.status IN('queued','running') AND(SELECT count(*)FROM photo.apply_nodes n JOIN photo.tasks t ON t.tenant_id=n.tenant_id AND t.run_id=n.run_id AND t.id=n.id AND t.operation=n.operation WHERE n.tenant_id=b.tenant_id AND n.run_id=b.run_id AND n.phase='local'AND n.operation='image.local')=3 AND EXISTS(SELECT FROM photo.tasks t JOIN photo.task_attempts a ON a.tenant_id=t.tenant_id AND a.task_id=t.id AND a.attempt=t.attempt AND a.fencing_token=t.fencing_token AND a.worker_id=t.worker_id WHERE t.id=$3 AND t.tenant_id=b.tenant_id AND t.run_id=b.run_id AND t.operation='image.local' AND t.state='failed' AND t.next_attempt_at IS NULL AND t.canonical_result_id IS NULL AND a.finished_at IS NOT NULL AND a.error->>'code'='INTERNAL_ERROR' AND a.error->'retryable'='false'::jsonb))`, s.Tenant, s.Run, oldFailedTask).Scan(&ok)
	if e != nil {
		return e
	}
	if !ok {
		return errors.New("exact saved failed predecessor not ready")
	}
	return nil
}
func (s scope) normalizeTerminal(table string, row map[string]json.RawMessage) {
	if text(row, "tenant_id") != s.Tenant.String() {
		return
	}
	var fields []string
	switch {
	case table == "runs" && text(row, "id") == s.Run.String():
		fields = []string{"status", "graph_sealed", "revision"}
	case table == "apply_builds" && text(row, "run_id") == s.Run.String():
		fields = []string{"phase", "lease_token", "lease_until", "last_error", "updated_at"}
	case table == "quota_reservations" && text(row, "holder_type") == "run" && text(row, "holder_id") == s.Run.String():
		fields = []string{"bytes", "state"}
	case table == "tenant_usage":
		fields = []string{"reserved_bytes"}
	}
	for _, f := range fields {
		delete(row, f)
	}
}
func (s scopedStore) FailTerminalPredecessor(ctx context.Context, l workflow.ApplyLease) (bool, error) {
	if !s.scope.terminal {
		return s.AuthoredBaseApplyStore.FailTerminalPredecessor(ctx, l)
	}
	tx, e := s.claimPool.Begin(ctx)
	if e != nil {
		return false, e
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if e = oldFailureReady(ctx, tx, s.scope); e != nil {
		return false, e
	}
	var revision, usage, quota int64
	if e = tx.QueryRow(ctx, `SELECT r.revision,u.reserved_bytes,coalesce((SELECT sum(q.bytes)FROM photo.quota_reservations q WHERE q.tenant_id=r.tenant_id AND q.holder_type='run'AND q.holder_id=r.id AND q.state='reserved'),0)::bigint FROM photo.runs r JOIN photo.tenant_usage u ON u.tenant_id=r.tenant_id WHERE r.tenant_id=$1 AND r.id=$2`, s.scope.Tenant, s.scope.Run).Scan(&revision, &usage, &quota); e != nil {
		return false, e
	}
	before, e := capture(ctx, tx, s.scope)
	if e != nil {
		return false, e
	}
	if _, e = tx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); e != nil {
		return false, e
	}
	failed, e := (workflow.ApplyStore{DB: tx}).FailTerminalPredecessor(ctx, l)
	if e != nil {
		return false, e
	}
	if !failed {
		return false, errors.New("normal public terminal gate refused")
	}
	if _, e = tx.Exec(ctx, "RESET ROLE"); e != nil {
		return false, e
	}
	after, e := capture(ctx, tx, s.scope)
	if e != nil {
		return false, e
	}
	if !equalSnapshots(before, after) {
		return false, errors.New("outside terminal effect refused")
	}
	var ok bool
	e = tx.QueryRow(ctx, `SELECT r.status='failed'AND r.graph_sealed AND NOT r.cancel_requested AND r.revision=$3+1 AND b.phase='failed'AND b.lease_token IS NULL AND b.lease_until IS NULL AND b.last_error='APPLY_STAGE_FAILED' AND u.reserved_bytes=$4-$5 AND NOT EXISTS(SELECT FROM photo.quota_reservations q WHERE q.tenant_id=r.tenant_id AND q.holder_type='run'AND q.holder_id=r.id AND(q.bytes<>0 OR q.state<>'released'))FROM photo.runs r JOIN photo.apply_builds b ON b.tenant_id=r.tenant_id AND b.run_id=r.id JOIN photo.tenant_usage u ON u.tenant_id=r.tenant_id WHERE r.tenant_id=$1 AND r.id=$2`, s.scope.Tenant, s.scope.Run, revision, usage, quota).Scan(&ok)
	if e != nil {
		return false, e
	}
	if !ok {
		return false, errors.New("exact terminal quota/run/build effect refused")
	}
	if e = tx.Commit(ctx); e != nil {
		return false, e
	}
	return true, nil
}
