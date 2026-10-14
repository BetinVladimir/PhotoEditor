package semanticproposal

import (
	"context"
	"errors"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestNativeInputTransferRegistrationRefusesBeforeSQL(t *testing.T) {
	reader := SQLTransferSourceReader{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := reader.registerNativeInputTransferInTransaction(ctx, nil, auth.Identity{}, Request{}, PublishedIntent{}, nativeInputTransferWorkflowPlan{}, TransferPreparation{}, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	err = reader.registerNativeInputTransferInTransaction(context.Background(), nil, auth.Identity{}, Request{}, PublishedIntent{}, nativeInputTransferWorkflowPlan{}, TransferPreparation{}, nil, nil)
	if !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("missing transaction: %v", err)
	}
	tx := &manualLockTx{role: "photo_scheduler"}
	err = reader.registerNativeInputTransferInTransaction(context.Background(), tx, auth.Identity{}, Request{}, PublishedIntent{}, nativeInputTransferWorkflowPlan{}, TransferPreparation{}, nil, nil)
	if !errors.Is(err, v2input.ErrInvalid) || len(tx.calls) != 0 {
		t.Fatalf("incomplete registration reached SQL: %v %v", err, tx.calls)
	}
}
