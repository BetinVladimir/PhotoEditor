package semanticproposal

import (
	"context"
	"testing"

	"photoeditor/backend/internal/auth"
)

func TestManualTransferReferenceDocumentsRefuseIncompleteProofBeforeSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	for _, current := range []context.Context{ctx, context.Background()} {
		out, err := reader.readNativeInputManualTransferReferenceDocuments(current, auth.Identity{}, Request{}, nativeInputManualReferencePerson{}, TransferColorProfile{}, 1)
		if err == nil || out.reference != nil || out.selected != nil || out.protection != nil {
			t.Fatal("incomplete manual reference exposed documents", err)
		}
		if current == ctx && err != context.Canceled {
			t.Fatal(err)
		}
		tx := &manualLockTx{role: "photo_scheduler"}
		err = reader.lockNativeInputManualTransferReference(current, tx, auth.Identity{}, Request{}, nativeInputManualTransferReferenceDocuments{}, TransferColorProfile{}, nil)
		if err == nil || len(tx.calls) != 0 {
			t.Fatal("incomplete manual reference reached SQL", err)
		}
		if current == ctx && err != context.Canceled {
			t.Fatal(err)
		}
		count, proof, err := reader.verifyNativeInputManualSelectionSeparation(current, auth.Identity{}, Request{}, nativeInputManualSourceContinuation{}, TransferColorProfile{})
		if err == nil || count != 0 || proof.count != 0 {
			t.Fatal("incomplete manual source exposed selection", err)
		}
		if current == ctx && err != context.Canceled {
			t.Fatal(err)
		}
	}
}
