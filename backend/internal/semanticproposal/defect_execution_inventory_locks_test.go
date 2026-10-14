package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestDefectExecutionInventoryIncompleteBeforeSQL(t *testing.T) {
	reader := SQLTransferSourceReader{}
	tx := &sourcePlanTx{manualLockTx: manualLockTx{role: "photo_scheduler"}}
	if err := reader.lockDefectExecutionInventoryInTransaction(context.Background(), tx, auth.Identity{}, uuid.New(), v2input.SessionVersionBinding{}, defectMaskExecutionPreparation{}); err != v2input.ErrInvalid || len(tx.calls) != 0 {
		t.Fatal("incomplete preparation acquired SQL locks", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := reader.lockDefectExecutionInventoryInTransaction(ctx, tx, auth.Identity{}, uuid.New(), v2input.SessionVersionBinding{}, defectMaskExecutionPreparation{}); !errors.Is(err, context.Canceled) || len(tx.calls) != 0 {
		t.Fatal("cancelled preparation acquired SQL locks", err)
	}
}
