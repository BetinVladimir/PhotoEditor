package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestDefectFaceExecutionRejectsMalformedRetainedProofsBeforeSQL(t *testing.T) {
	reader := SQLTransferSourceReader{}
	for _, count := range []int{-1, 0, 65} {
		tx := &sourcePlanTx{manualLockTx: manualLockTx{role: "photo_scheduler"}}
		err := reader.lockDefectExecutionFacesInTransaction(context.Background(), tx, auth.Identity{}, uuid.New(), v2input.SessionVersionBinding{}, defectFaceExecutionPreparation{producerCount: count})
		if err != v2input.ErrInvalid || len(tx.calls) != 0 {
			t.Fatal("invalid preparation reached SQL", count, err, tx.calls)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx := &sourcePlanTx{manualLockTx: manualLockTx{role: "photo_scheduler"}}
	if err := reader.lockDefectExecutionFacesInTransaction(ctx, tx, auth.Identity{}, uuid.New(), v2input.SessionVersionBinding{}, defectFaceExecutionPreparation{}); !errors.Is(err, context.Canceled) || len(tx.calls) != 0 {
		t.Fatal("cancelled preparation reached SQL", err)
	}
}

func TestDefectParserExecutionRejectsMissingEvidenceBeforeSQL(t *testing.T) {
	reader := SQLTransferSourceReader{}
	for _, count := range []int{-1, 0, 65} {
		tx := &sourcePlanTx{manualLockTx: manualLockTx{role: "photo_scheduler"}}
		err := reader.lockDefectExecutionParsersInTransaction(context.Background(), tx, auth.Identity{}, uuid.New(), v2input.SessionVersionBinding{}, defectMaskExecutionPreparation{parserCount: count})
		if err != v2input.ErrInvalid || len(tx.calls) != 0 {
			t.Fatal("invalid parser preparation reached SQL", count, err, tx.calls)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx := &sourcePlanTx{manualLockTx: manualLockTx{role: "photo_scheduler"}}
	if err := reader.lockDefectExecutionParsersInTransaction(ctx, tx, auth.Identity{}, uuid.New(), v2input.SessionVersionBinding{}, defectMaskExecutionPreparation{}); !errors.Is(err, context.Canceled) || len(tx.calls) != 0 {
		t.Fatal("cancelled parser preparation reached SQL", err)
	}
}

func TestDefectManualExecutionRejectsIncompletePreparationBeforeSQL(t *testing.T) {
	reader := SQLTransferSourceReader{}
	for _, count := range []int{-1, 0, 65} {
		tx := &sourcePlanTx{manualLockTx: manualLockTx{role: "photo_scheduler"}}
		err := reader.lockDefectExecutionManualInTransaction(context.Background(), tx, auth.Identity{}, uuid.New(), v2input.SessionVersionBinding{}, defectMaskExecutionPreparation{manualCount: count})
		if err != v2input.ErrInvalid || len(tx.calls) != 0 {
			t.Fatal("invalid manual execution reached SQL", count, err, tx.calls)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx := &sourcePlanTx{manualLockTx: manualLockTx{role: "photo_scheduler"}}
	if err := reader.lockDefectExecutionManualInTransaction(ctx, tx, auth.Identity{}, uuid.New(), v2input.SessionVersionBinding{}, defectMaskExecutionPreparation{}); !errors.Is(err, context.Canceled) || len(tx.calls) != 0 {
		t.Fatal("cancelled manual execution reached SQL", err)
	}
}
