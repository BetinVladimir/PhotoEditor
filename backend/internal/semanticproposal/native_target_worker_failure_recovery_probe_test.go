package semanticproposal

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
)

// Unlike the metadata matrix, the target was prepared, registered, sealed,
// dispatched and claimed before the worker DB protocol committed its error.
func probeNativeTargetWorkerFailureRecovery(t *testing.T, ctx context.Context, p TransferDraftPreparer, who auth.Identity, d detectorDraftPlan, run, task uuid.UUID, operation string) {
	t.Helper()
	owner, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	var state string
	if err := owner.QueryRow(ctx, "SELECT state FROM photo.tasks WHERE id=$1", task).Scan(&state); err != nil || state != "failed" {
		t.Fatal("target worker failure not terminal", state, err)
	}
	if observed, err := p.Reader.readNativeInitialTerminalState(ctx, who, d.request.TenantID, d.request.RunID); err != nil || observed != operation {
		t.Fatal("target failure observation", observed, err)
	}
	probeNativeTerminalRecorderLocks(t, ctx, p, owner, d, run)
	objects := &nativeTargetTerminalNoIO{}
	publisher := &nativeRefusedRepairPublisher{}
	p.Reader.Objects = objects
	p.Documents.Objects = objects
	p.TransportDocuments = publisher
	catalogs := 0
	factory := func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID) (v2draft.ArtifactCatalog, error) {
		catalogs++
		return nil, errors.New("terminal target catalog IO")
	}
	job := SQLNativeInitialPipelineRecovery{Preparer: p, CatalogForReference: factory, Timeout: time.Minute}
	if worked, err := job.ProcessOne(ctx); err != nil || !worked {
		t.Fatal("target failed ProcessOne", worked, err)
	}
	if worked, err := job.ProcessOne(ctx); err != nil || worked {
		t.Fatal("target failed reclaimed", worked, err)
	}
	if objects.calls != 0 || publisher.calls != 0 || catalogs != 0 {
		t.Fatal("target failure repaired inputs", objects.calls, publisher.calls, catalogs)
	}
	t.Log("V27_TARGET_WORKER_FAILURE_RECOVERY_OK:" + operation)
}

func probeNativeTargetFailureIfRequested(t *testing.T, ctx context.Context, p TransferDraftPreparer, who auth.Identity, d detectorDraftPlan, run, task uuid.UUID, operation string) bool {
	t.Helper()
	wanted := os.Getenv("PHOTO_TEST_V27_TARGET_WORKER_FAILURE_OPERATION")
	if wanted == "" && os.Getenv("PHOTO_TEST_V27_TARGET_PERSON_WORKER_FAILURE") == "1" {
		wanted = "mask.person"
	}
	if wanted != operation {
		return false
	}
	t.Setenv("PHOTO_TEST_V27_NATIVE_TERMINAL_WORKER_FAILURE", "1")
	probeNativeWireTaskOutputs(t, ctx, task)
	probeNativeTargetWorkerFailureRecovery(t, ctx, p, who, d, run, task, operation)
	return true
}
