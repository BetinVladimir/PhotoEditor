package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestNativeInputTransferBeginRejectsBeforeSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	admission, err := reader.beginNativeInputTransferInTransaction(ctx, nil, auth.Identity{}, Request{}, "", PublishedIntent{}, nativeInputTransferWorkflowPlan{}, TransferPreparation{}, nil, 0)
	if !errors.Is(err, context.Canceled) || admission.RunID != uuid.Nil {
		t.Fatalf("cancelled begin exposed admission: %v", err)
	}
	tx := &manualLockTx{role: "photo_scheduler"}
	admission, err = reader.beginNativeInputTransferInTransaction(context.Background(), tx, auth.Identity{}, Request{}, "", PublishedIntent{}, nativeInputTransferWorkflowPlan{}, TransferPreparation{}, nil, 0)
	if !errors.Is(err, v2input.ErrInvalid) || admission.RunID != uuid.Nil || len(tx.calls) != 0 {
		t.Fatalf("invalid begin reached SQL: %v", err)
	}
}

func TestNativeInputTransferDraftOutcomeRequiresBlockedState(t *testing.T) {
	run := uuid.New()
	for _, state := range []string{"created", "pending"} {
		admission, err := nativeInputTransferDraftOutcome(run, run, state)
		if err != nil || admission.RunID != run || admission.Outcome != state {
			t.Fatalf("blocked state refused: %v", err)
		}
	}
	for _, state := range []string{"replayed", "conflict", "unknown", ""} {
		admission, err := nativeInputTransferDraftOutcome(run, run, state)
		if !errors.Is(err, v2input.ErrConflict) || admission.RunID != uuid.Nil {
			t.Fatalf("accepted %s: %v", state, err)
		}
	}
	admission, err := nativeInputTransferDraftOutcome(run, uuid.New(), "created")
	if !errors.Is(err, v2input.ErrInvalid) || admission.RunID != uuid.Nil {
		t.Fatal("foreign run accepted")
	}
	admission, err = nativeInputTransferDraftOutcome(run, run, "expired")
	if !errors.Is(err, v2input.ErrExpired) || admission.RunID != uuid.Nil {
		t.Fatal("expired run accepted")
	}
}
