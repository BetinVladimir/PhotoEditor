package semanticproposal

import (
	"context"
	"testing"

	"photoeditor/backend/internal/auth"
)

func TestManualTransferRegistrationRefusesIncompleteGraphBeforeSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	for _, current := range []context.Context{ctx, context.Background()} {
		tx := &manualLockTx{role: "photo_scheduler"}
		draft, err := reader.beginNativeInputManualTransferInTransaction(current, tx, auth.Identity{}, Request{}, "", PublishedIntent{}, nativeInputManualTransferWorkflowPlan{}, TransferPreparation{}, nil, 1)
		if err == nil || draft.Outcome != "" || len(tx.calls) != 0 {
			t.Fatal("incomplete manual graph exposed draft", err)
		}
		if current == ctx && err != context.Canceled {
			t.Fatal(err)
		}
		err = reader.registerNativeInputManualTransferInTransaction(current, tx, auth.Identity{}, Request{}, PublishedIntent{}, nativeInputManualTransferWorkflowPlan{}, TransferPreparation{}, nil, nil)
		if err == nil || len(tx.calls) != 0 {
			t.Fatal("incomplete manual graph reached registration", err)
		}
		if current == ctx && err != context.Canceled {
			t.Fatal(err)
		}
	}
}
