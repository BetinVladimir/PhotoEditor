package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestNativeInputManualTransferSealCancelledBeforeSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	got, err := reader.sealNativeInputManualTransferInTransaction(ctx, nil, auth.Identity{}, Request{}, "", PublishedIntent{}, nativeInputManualTransferWorkflowPlan{}, TransferPreparation{}, nil, nil)
	if !errors.Is(err, context.Canceled) || got.RunID != uuid.Nil {
		t.Fatalf("cancelled seal: %v", err)
	}
	got, err = reader.activateNativeInputManualTransfer(context.Background(), nil, auth.Identity{}, Request{}, "", PublishedIntent{}, nativeInputManualTransferWorkflowPlan{}, TransferPreparation{}, v2input.Built{}, nil)
	if !errors.Is(err, v2input.ErrInvalid) || got.RunID != uuid.Nil {
		t.Fatalf("missing scheduler: %v", err)
	}
}
