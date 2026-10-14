package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestNativeInputTransferRecoveryRefusesBeforeSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	ticket, err := reader.recoverNativeInputTransferInTransaction(ctx, nil, auth.Identity{}, Request{}, "", PublishedIntent{}, nativeInputTransferWorkflowPlan{}, TransferPreparation{}, nil)
	if !errors.Is(err, context.Canceled) || ticket.RunID != uuid.Nil {
		t.Fatalf("cancelled recovery: %v", err)
	}
	tx := &manualLockTx{role: "photo_scheduler"}
	ticket, err = reader.recoverNativeInputTransferInTransaction(context.Background(), tx, auth.Identity{}, Request{}, "", PublishedIntent{}, nativeInputTransferWorkflowPlan{}, TransferPreparation{}, nil)
	if !errors.Is(err, v2input.ErrInvalid) || ticket.RunID != uuid.Nil || len(tx.calls) != 0 {
		t.Fatalf("incomplete recovery reached SQL: %v", err)
	}
}

func TestNativeInputTransferPhysicalAndResumeRejectBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	if err := reader.recheckNativeInputTransferPhysical(ctx, auth.Identity{}, Request{}, nativeInputTransferWorkflowPlan{}, TransferPreparation{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	ticket, err := reader.resumeNativeInputTransfer(context.Background(), nil, auth.Identity{}, Request{}, "", PublishedIntent{}, nativeInputTransferWorkflowPlan{}, TransferPreparation{}, v2input.Built{})
	if !errors.Is(err, v2input.ErrInvalid) || ticket.RunID != uuid.Nil {
		t.Fatalf("resume without scheduler: %v", err)
	}
}
