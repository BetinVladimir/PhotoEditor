package semanticproposal

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Owner requests task-local cancellation; only the production scheduler changes
// task state and records the terminal handoff. No terminal metadata is forged.
func probeNativeTerminalCancellation(t *testing.T, ctx context.Context, reader SQLTransferSourceReader, scheduler *pgxpool.Pool, who auth.Identity, detector detectorDraftPlan) {
	t.Helper()
	pixels := []v2input.Ref{detector.request.Intent.Reference.Source}
	diameter := [2]float64{.002, .012}
	control, err := scheduler.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Rollback(context.WithoutCancel(ctx))
	if err = reader.saveNativeManualSelectionInTransaction(ctx, control, who, detector, pixels, diameter); err != nil {
		t.Fatal("live checkpoint positive control", err)
	}
	if err = control.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	presence, err := reader.readNativeManualSelectionPresence(ctx, who, detector)
	if err != nil || presence != nativeStageAbsent {
		t.Fatal("positive control rollback", err)
	}
	var tenant, claimed, parent, actor, session, token uuid.UUID
	var epoch int64
	if err = scheduler.QueryRow(ctx, "SELECT tenant_id,detector_run_id,parent_run_id,actor_id,session_id,auth_epoch,lease_token FROM photo.claim_v27_native_initial_pipeline($1,60)", reader.ProjectID).Scan(&tenant, &claimed, &parent, &actor, &session, &epoch, &token); err != nil || tenant != detector.request.TenantID || claimed != detector.shell.Run || parent != detector.request.RunID || actor != who.UserID || session != who.SessionID || token == uuid.Nil {
		t.Fatal("terminal probe lease", err)
	}
	var recorded bool
	if err = scheduler.QueryRow(ctx, "SELECT photo.record_v27_native_initial_pipeline_handoff($1,$2,$3,'failed')", tenant, claimed, token).Scan(&recorded); err != nil || recorded {
		t.Fatal("healthy child recorded failure", err)
	}
	owner, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	changed, err := owner.Exec(ctx, "UPDATE photo.tasks SET cancel_requested=true WHERE tenant_id=$1 AND run_id=$2 AND state='ready'", tenant, claimed)
	if err != nil || changed.RowsAffected() != 1 {
		t.Fatal("task cancellation request", err)
	}
	var cancelled int
	if err = scheduler.QueryRow(ctx, "SELECT photo.cancel_idle_task()").Scan(&cancelled); err != nil || cancelled != 1 {
		t.Fatal("scheduler cancellation", err)
	}
	var state string
	var fence int64
	var result *uuid.UUID
	if err = owner.QueryRow(ctx, "SELECT state,fencing_token,canonical_result_id FROM photo.tasks WHERE tenant_id=$1 AND run_id=$2", tenant, claimed).Scan(&state, &fence, &result); err != nil || state != "cancelled" || fence != 1 || result != nil {
		t.Fatal("real cancelled child", state, fence, err)
	}
	if err = scheduler.QueryRow(ctx, "SELECT photo.record_v27_native_initial_pipeline_handoff($1,$2,$3,'completed')", tenant, claimed, token).Scan(&recorded); err != nil || recorded {
		t.Fatal("cancelled child completed handoff", err)
	}
	if err = scheduler.QueryRow(ctx, "SELECT photo.record_v27_native_initial_pipeline_handoff($1,$2,$3,'failed')", tenant, claimed, uuid.New()).Scan(&recorded); err != nil || recorded {
		t.Fatal("stale terminal token", err)
	}
	if err = scheduler.QueryRow(ctx, "SELECT photo.record_v27_native_initial_pipeline_handoff($1,$2,$3,'failed')", tenant, claimed, token).Scan(&recorded); err != nil || !recorded {
		t.Fatal("cancelled child terminal recording", err)
	}
	if err = reader.saveNativeManualSelection(ctx, scheduler, who, detector, pixels, diameter); !errors.Is(err, v2input.ErrConflict) {
		t.Fatal("terminal child accepted late checkpoint", err)
	}
	var done, runCancelled bool
	var lease *uuid.UUID
	var selection *string
	if err = owner.QueryRow(ctx, "SELECT v27_initial_recovery_done,cancel_requested,v27_initial_recovery_token,v27_initial_manual_selection FROM photo.runs WHERE tenant_id=$1 AND id=$2", tenant, claimed).Scan(&done, &runCancelled, &lease, &selection); err != nil || !done || runCancelled || lease != nil || selection != nil {
		t.Fatal("terminal-only gate snapshot", err)
	}
	if err = scheduler.QueryRow(ctx, "SELECT photo.record_v27_native_initial_pipeline_handoff($1,$2,$3,'failed')", tenant, claimed, token).Scan(&recorded); err != nil || recorded {
		t.Fatal("retired token replay", err)
	}
	t.Log("V27_NATIVE_TERMINAL_CANCEL_LATE_SELECTION_DENIED_OK")
}
