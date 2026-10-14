package semanticproposal

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	runservice "photoeditor/backend/internal/run"
	"photoeditor/backend/internal/v2input"
)

func probeNativeTerminalProcessOne(t *testing.T, ctx context.Context, p TransferDraftPreparer, who auth.Identity, detector detectorDraftPlan, operation string) {
	t.Helper()
	if operation != "face.detect" && operation != "mask.person" {
		t.Fatal("terminal probe operation")
	}
	if observed, err := p.Reader.readNativeInitialTerminalState(ctx, who, detector.request.TenantID, detector.request.RunID); err != nil || observed != "" {
		t.Fatal("healthy terminal observation", observed, err)
	}
	foreign := who
	foreign.SessionID = uuid.New()
	if observed, err := p.Reader.readNativeInitialTerminalState(ctx, foreign, detector.request.TenantID, detector.request.RunID); err == nil || observed != "" {
		t.Fatal("foreign terminal authority", err)
	}
	if observed, err := p.Reader.readNativeInitialTerminalState(ctx, who, detector.request.TenantID, uuid.New()); err == nil || observed != "" {
		t.Fatal("foreign parent terminal authority", err)
	}
	pixels := []v2input.Ref{detector.request.Intent.Reference.Source}
	diameter := [2]float64{.002, .012}
	control, err := p.Scheduler.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Rollback(context.WithoutCancel(ctx))
	if err = p.Reader.saveNativeManualSelectionInTransaction(ctx, control, who, detector, pixels, diameter); err != nil {
		t.Fatal("checkpoint control", err)
	}
	if err = control.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	run, _, _, err := nativeInputSavedStageIdentity(detector.request.RunID, operation)
	if err != nil {
		t.Fatal(err)
	}
	draft := os.Getenv("PHOTO_TEST_V27_NATIVE_TERMINAL_DRAFT") == "1"
	var draftBefore string
	if draft {
		draftBefore = observeNativeTerminalDraft(t, ctx, owner, detector.request.TenantID, run)
	}
	workerFailure := os.Getenv("PHOTO_TEST_V27_NATIVE_TERMINAL_WORKER_FAILURE") == "1"
	if workerFailure {
		_, task, _, err := nativeInputSavedStageIdentity(detector.request.RunID, operation)
		if err != nil {
			t.Fatal(err)
		}
		probeNativeWireTaskOutputs(t, ctx, task)
		var state string
		if err := owner.QueryRow(ctx, "SELECT state FROM photo.tasks WHERE id=$1", task).Scan(&state); err != nil || state != "failed" {
			t.Fatal("nonretryable worker error not terminal", state, err)
		}
	} else if operation == "mask.person" {
		status, err := (runservice.Service{DB: p.Reader.DB}).Cancel(ctx, who, detector.request.TenantID, run)
		if err != nil || status.State != "cancelling" {
			t.Fatal("authenticated original person cancellation", err)
		}
	} else {
		changed, err := owner.Exec(ctx, "UPDATE photo.tasks SET cancel_requested=true WHERE tenant_id=$1 AND run_id=$2 AND operation=$3 AND state='ready'", detector.request.TenantID, run, operation)
		if err != nil || changed.RowsAffected() != 1 {
			t.Fatal("terminal cancellation request", err)
		}
		if observed, err := p.Reader.readNativeInitialTerminalState(ctx, who, detector.request.TenantID, detector.request.RunID); err != nil || observed != "" {
			t.Fatal("request alone was terminal task evidence", observed, err)
		}
	}
	if draft {
		if observed, err := p.Reader.readNativeInitialTerminalState(ctx, who, detector.request.TenantID, detector.request.RunID); err != nil || observed != operation {
			t.Fatal("unsealed cancellation observation", observed, err)
		}
	}
	if !workerFailure {
		var n int
		if err = p.Scheduler.QueryRow(ctx, "SELECT photo.cancel_idle_task()").Scan(&n); err != nil || n != 1 {
			t.Fatal("terminal scheduler cancellation", err)
		}
	}
	if observed, err := p.Reader.readNativeInitialTerminalState(ctx, who, detector.request.TenantID, detector.request.RunID); err != nil || observed != operation {
		t.Fatal("cancelled original observation", observed, err)
	}
	probeNativeTerminalRecorderLocks(t, ctx, p, owner, detector, run)
	spy := &nativeRefusedRepairPublisher{}
	p.TransportDocuments = spy
	factory := (SQLNativeParentArtifactCatalogFactory{API: p.Reader.DB, Bucket: p.Reader.Bucket}).ForReference
	job := SQLNativeInitialPipelineRecovery{Preparer: p, CatalogForReference: factory, Timeout: time.Minute}
	if worked, err := job.ProcessOne(ctx); err != nil || !worked {
		t.Fatal("terminal ProcessOne", worked, err)
	}
	if worked, err := job.ProcessOne(ctx); err != nil || worked {
		t.Fatal("terminal checkpoint reclaimed", worked, err)
	}
	if workerFailure && operation == "mask.person" {
		admission := SQLNativePersonAdmissionRecovery{SQLNativeInitialPipelineRecovery: job}
		if worked, err := admission.ProcessOne(ctx); err != nil || !worked {
			t.Fatal("failed executed person admission receipt retirement", worked, err)
		}
		if worked, err := admission.ProcessOne(ctx); err != nil || worked {
			t.Fatal("executed person admission reclaimed", worked, err)
		}
		t.Log("V27_NATIVE_FAILED_PERSON_ADMISSION_RETIRED_OK")
	}
	if spy.calls != 0 {
		t.Fatal("terminal recovery published transport", spy.calls)
	}
	if err = p.Reader.saveNativeManualSelection(ctx, p.Scheduler, who, detector, pixels, diameter); !errors.Is(err, v2input.ErrConflict) {
		t.Fatal("terminal job accepted late selection", err)
	}
	var done, runCancelled bool
	var token *uuid.UUID
	var selection *string
	if err = owner.QueryRow(ctx, "SELECT v27_initial_recovery_done,v27_initial_recovery_token,v27_initial_manual_selection,cancel_requested FROM photo.runs WHERE tenant_id=$1 AND id=$2", detector.request.TenantID, detector.shell.Run).Scan(&done, &token, &selection, &runCancelled); err != nil || !done || token != nil || selection != nil || runCancelled {
		t.Fatal("terminal job durable state", err)
	}
	if draft {
		if after := observeNativeTerminalDraft(t, ctx, owner, detector.request.TenantID, run); after != draftBefore {
			t.Fatal("terminal draft recovery changed frozen inputs")
		}
		t.Log("V27_NATIVE_TERMINAL_UNSEALED_DRAFT_OK")
	}
	if workerFailure {
		t.Log("V27_NATIVE_TERMINAL_WORKER_FAILURE_OK")
	}
	t.Log("V27_NATIVE_TERMINAL_PROCESS_ONE_OK")
}

// A rolled-back terminal handoff must hold the cancelled task against a retry
// writer. The same writer succeeds after release, then rolls back its control.
func probeNativeTerminalRecorderLocks(t *testing.T, ctx context.Context, p TransferDraftPreparer, owner *pgxpool.Pool, detector detectorDraftPlan, terminalRun uuid.UUID) {
	t.Helper()
	tx, err := p.Scheduler.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var claimed, token uuid.UUID
	if err = tx.QueryRow(ctx, "SELECT detector_run_id,lease_token FROM photo.claim_v27_native_initial_pipeline($1,60)", p.Reader.ProjectID).Scan(&claimed, &token); err != nil || claimed != detector.shell.Run {
		t.Fatal("terminal lock lease", err)
	}
	var recorded bool
	if err = tx.QueryRow(ctx, "SELECT photo.record_v27_native_initial_pipeline_handoff($1,$2,$3,'failed')", detector.request.TenantID, claimed, token).Scan(&recorded); err != nil || !recorded {
		t.Fatal("terminal locked record", err)
	}
	writer, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback(context.WithoutCancel(ctx))
	var readerPID, writerPID int
	if err = tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&readerPID); err != nil {
		t.Fatal(err)
	}
	if err = writer.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&writerPID); err != nil || writerPID == readerPID {
		t.Fatal("independent terminal writer", err)
	}
	if _, err = writer.Exec(ctx, "SET LOCAL lock_timeout='150ms'"); err != nil {
		t.Fatal(err)
	}
	_, err = writer.Exec(ctx, "UPDATE photo.tasks SET cancel_requested=false WHERE tenant_id=$1 AND run_id=$2", detector.request.TenantID, terminalRun)
	var postgres *pgconn.PgError
	if !errors.As(err, &postgres) || postgres.Code != "55P03" {
		t.Fatal("terminal retry writer was not blocked", err)
	}
	if err = writer.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	control, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Rollback(context.WithoutCancel(ctx))
	changed, err := control.Exec(ctx, "UPDATE photo.tasks SET cancel_requested=false WHERE tenant_id=$1 AND run_id=$2", detector.request.TenantID, terminalRun)
	if err != nil || changed.RowsAffected() != 1 {
		t.Fatal("released terminal writer control", err)
	}
	if err = control.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	t.Log("V27_NATIVE_TERMINAL_RECORDER_WRITER_LOCK_OK")
}

func observeNativeTerminalDraft(t *testing.T, ctx context.Context, owner *pgxpool.Pool, tenantID, runID uuid.UUID) string {
	t.Helper()
	var value string
	err := owner.QueryRow(ctx, `SELECT jsonb_build_object('config',r.config_snapshot,'hash',r.config_hash,'params',t.params_artifact_id,'digest',t.input_digest,'profile',t.execution_profile)::text FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE r.tenant_id=$1 AND r.id=$2`, tenantID, runID).Scan(&value)
	if err != nil {
		t.Fatal("observe original draft", err)
	}
	return value
}
