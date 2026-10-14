package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestNativeInputManualTransferReplayRejectsCancelledAndIncomplete(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	ticket, err := reader.replayNativeInputManualTransferInTransaction(ctx, nil, auth.Identity{}, Request{}, "", PublishedIntent{}, nativeInputManualTransferWorkflowPlan{}, TransferPreparation{}, nil, nil)
	if !errors.Is(err, context.Canceled) || ticket.RunID != uuid.Nil {
		t.Fatalf("cancelled replay exposed ticket: %v", err)
	}
	ticket, err = reader.replayNativeInputManualTransferInTransaction(context.Background(), nil, auth.Identity{}, Request{}, "", PublishedIntent{}, nativeInputManualTransferWorkflowPlan{}, TransferPreparation{}, nil, nil)
	if !errors.Is(err, v2input.ErrInvalid) || ticket.RunID != uuid.Nil {
		t.Fatalf("missing transaction accepted: %v", err)
	}
	tx := &manualLockTx{role: "photo_scheduler"}
	ticket, err = reader.replayNativeInputManualTransferInTransaction(context.Background(), tx, auth.Identity{}, Request{}, "", PublishedIntent{}, nativeInputManualTransferWorkflowPlan{}, TransferPreparation{}, nil, nil)
	if err == nil || ticket.RunID != uuid.Nil || len(tx.calls) != 0 {
		t.Fatalf("incomplete replay reached SQL: %v", err)
	}
}
