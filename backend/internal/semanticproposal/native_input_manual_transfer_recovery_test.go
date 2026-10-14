package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestNativeInputManualTransferRecoveryRefusesBeforeSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	ticket, err := reader.recoverNativeInputManualTransferInTransaction(ctx, nil, auth.Identity{}, Request{}, "", PublishedIntent{}, nativeInputManualTransferWorkflowPlan{}, TransferPreparation{}, nil)
	if !errors.Is(err, context.Canceled) || ticket.RunID != uuid.Nil {
		t.Fatalf("cancelled recovery: %v", err)
	}
	tx := &manualLockTx{role: "photo_scheduler"}
	ticket, err = reader.recoverNativeInputManualTransferInTransaction(context.Background(), tx, auth.Identity{}, Request{}, "", PublishedIntent{}, nativeInputManualTransferWorkflowPlan{}, TransferPreparation{}, nil)
	if !errors.Is(err, v2input.ErrInvalid) || ticket.RunID != uuid.Nil || len(tx.calls) != 0 {
		t.Fatalf("incomplete recovery reached SQL: %v", err)
	}
}

func TestNativeInputManualTransferPhysicalAndResumeRejectBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	if err := reader.recheckNativeInputManualTransferPhysical(ctx, auth.Identity{}, Request{}, nativeInputManualTransferWorkflowPlan{}, TransferPreparation{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	ticket, err := reader.resumeNativeInputManualTransfer(context.Background(), nil, auth.Identity{}, Request{}, "", PublishedIntent{}, nativeInputManualTransferWorkflowPlan{}, TransferPreparation{}, v2input.Built{})
	if !errors.Is(err, v2input.ErrInvalid) || ticket.RunID != uuid.Nil {
		t.Fatalf("resume without scheduler: %v", err)
	}
}
