package semanticproposal

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// A separate backend attempts the same mutation before and after the actual
// preflight transaction. The positive control is rolled back, preserving data.
func probeNativeRegisteredPreflightLocks(t *testing.T, ctx context.Context, owner *pgxpool.Pool, p TransferDraftPreparer, who auth.Identity, original detectorDraftPlan, plan nativeAllTargetTransferWorkflowPlan, c TransferPreparation, built v2input.Built, artifact uuid.UUID) {
	t.Helper()
	prep := plan.reference.person.source.protection.published.preparation
	completed, err := p.Reader.marshalNativeDefectCompletedEvidence(ctx, who, prep.plan, c.Color, prep.result, built)
	if err != nil {
		t.Fatal(err)
	}
	before := observeNativeMixedParent(t, ctx, owner, original, true)
	reader, err := p.Scheduler.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Rollback(context.WithoutCancel(ctx))
	if err = p.Reader.lockNativeAllTargetTransferAdmissionEvidence(ctx, reader, who, original.request, original.published, plan, c, completed); err != nil {
		t.Fatal(err)
	}
	var state nativeTransferRepairState
	err = reader.QueryRow(ctx, "SELECT graph_sealed,needs_publication,reserved_bytes,registered_tasks FROM photo.lock_semantic_native_targets_transfer_repair_state_v3($1,$2,$3,$4,$5,$6,$7)", original.request.TenantID, p.Reader.ProjectID, who.UserID, who.SessionID, original.request.RunID, original.request.RequestSHA, plan.core.configSHA).Scan(&state.sealed, &state.missing, &state.reserved, &state.registered)
	if err != nil || state.sealed || !state.missing || len(state.registered) != 1 {
		t.Fatal("preflight lock baseline", state, err)
	}
	var readerPID, writerPID int
	if err = reader.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&readerPID); err != nil {
		t.Fatal(err)
	}
	writer, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback(context.WithoutCancel(ctx))
	if err = writer.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&writerPID); err != nil || readerPID == writerPID {
		t.Fatal("independent writer required", err)
	}
	if _, err = writer.Exec(ctx, "SET LOCAL lock_timeout='150ms'"); err != nil {
		t.Fatal(err)
	}
	mutation := "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2"
	_, err = writer.Exec(ctx, mutation, original.request.TenantID, artifact)
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) || pgerr.Code != "55P03" {
		t.Fatal("registered input was not protected against concurrent writer", err)
	}
	if err = writer.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err = reader.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	retry, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer retry.Rollback(context.WithoutCancel(ctx))
	if _, err = retry.Exec(ctx, "SET LOCAL lock_timeout='150ms'"); err != nil {
		t.Fatal(err)
	}
	changed, err := retry.Exec(ctx, mutation, original.request.TenantID, artifact)
	if err != nil || changed.RowsAffected() != 1 {
		t.Fatal("writer did not proceed after preflight release", changed, err)
	}
	if err = retry.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if observeNativeMixedParent(t, ctx, owner, original, true) != before {
		t.Fatal("concurrent preflight probe changed parent")
	}
	t.Log("V27_NATIVE_REGISTERED_PREFLIGHT_CONCURRENT_WRITER_BLOCKED_OK")
}
