package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"reflect"
	"testing"
)

func TestInitialDetectorCancellationBeforePublication(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plan, ticket, err := (nativePreparationBootstrap{}).bootstrapInitialDetector(ctx, auth.Identity{}, nativePreparationAnchor{}, v2input.Ref{}, .8, 20, nativePreparationBudget{})
	if !errors.Is(err, context.Canceled) || plan.seal != "" || ticket != (v2input.JobTicket{}) {
		t.Fatal("cancelled initial bootstrap exposed receipt", err)
	}
	ticket, err = (SQLTransferSourceReader{}).resumeInitialDetectorPreparation(ctx, nil, auth.Identity{}, detectorDraftPlan{})
	if !errors.Is(err, context.Canceled) || ticket != (v2input.JobTicket{}) {
		t.Fatal("cancelled initial replay accessed SQL", err)
	}
}

func TestInitialManualDetectorCancellationBeforePublication(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plan, ticket, err := (nativePreparationBootstrap{}).bootstrapInitialDetectorWithManualSelection(ctx, auth.Identity{}, nativePreparationAnchor{}, v2input.Ref{}, .8, 20, nativePreparationBudget{}, nil, [2]float64{})
	if !errors.Is(err, context.Canceled) || plan.seal != "" || ticket != (v2input.JobTicket{}) {
		t.Fatal("cancelled manual bootstrap exposed receipt", err)
	}
	ticket, err = (SQLTransferSourceReader{}).resumeInitialDetectorWithManualSelection(ctx, nil, auth.Identity{}, detectorDraftPlan{}, nil, [2]float64{})
	if !errors.Is(err, context.Canceled) || ticket != (v2input.JobTicket{}) {
		t.Fatal("cancelled manual admission accessed SQL", err)
	}
}

func TestCheckpointManualChildEnsureCancellationBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	submission, err := (nativePreparationBootstrap{}).ensureCheckpointSelectedManualDefect(ctx, auth.Identity{}, nativeInputRestoredFaceProducers{}, TransferPreparation{}, 1, nil)
	if !errors.Is(err, context.Canceled) || submission.plan.seal != "" || submission.ticket != (v2input.JobTicket{}) {
		t.Fatal("cancelled ensure exposed child", err)
	}
}

func TestManualCheckpointMatchCancellationBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := (SQLTransferSourceReader{}).verifyCheckpointManualDefectSelection(ctx, auth.Identity{}, nativeInputRestoredFaceProducers{}, TransferColorProfile{}, nativeInputManualInitialDraftPlan{})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled match accessed original inputs", err)
	}
}

func TestInitialManualTerminalOutcomeDoesNotRetry(t *testing.T) {
	outcome, err := nativeInitialPipelineRecoveryOutcome(uuid.New(), nativeFacePipelineAdvance{phase: nativePreparationFailed, operation: "defects.propose"}, nil)
	if err != nil || outcome != "failed" {
		t.Fatal("terminal manual child retried", outcome, err)
	}
	outcome, err = nativeInitialPipelineRecoveryOutcome(uuid.New(), nativeFacePipelineAdvance{phase: nativePreparationWaiting, operation: "defects.propose"}, nil)
	if err != nil || outcome != "waiting" {
		t.Fatal("pending defect lost scheduling checkpoint", outcome, err)
	}
}

func TestInitialAnchorCancellationBeforePublication(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	anchor, err := (nativePreparationBootstrap{}).prepareInitialNativeAnchor(ctx, auth.Identity{}, uuid.New(), "new", nil, TransferPreparation{}, 1)
	if !errors.Is(err, context.Canceled) || anchor.Key != "" || anchor.SnapshotBody != nil {
		t.Fatal("cancelled anchor preparation published inputs", err)
	}
}

func TestExistingInitialManualDetectorCancellationBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plan, ticket, err := (nativePreparationBootstrap{}).resumeExistingInitialManualDetector(ctx, auth.Identity{}, Request{}, "", TransferPreparation{}, 1, nil, nil, [2]float64{})
	if !errors.Is(err, context.Canceled) || plan.seal != "" || ticket != (v2input.JobTicket{}) {
		t.Fatal("cancelled resume exposed detector receipt", err)
	}
}

func TestInitialManualPersistenceCancellationBeforeDraft(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	prepared, err := (SQLTransferSourceReader{}).persistInitialManualDetectorInputs(ctx, nil, nil, auth.Identity{}, detectorDraftPlan{}, TransferColorProfile{}, nil, [2]float64{})
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(prepared.Graph.Built, v2input.Built{}) {
		t.Fatal("cancelled initial persistence exposed transport", err)
	}
}

func TestInitialManualCoordinatorCancellationBeforeLock(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plan, ticket, locked, err := (nativePreparationBootstrap{}).startOrResumeInitialManualDetector(ctx, auth.Identity{}, Request{}, "", TransferPreparation{}, 1, nil, .8, 20, nil, [2]float64{})
	if !errors.Is(err, context.Canceled) || locked || plan.seal != "" || ticket != (v2input.JobTicket{}) {
		t.Fatal("cancelled coordinator exposed lock or receipt", err)
	}
}
