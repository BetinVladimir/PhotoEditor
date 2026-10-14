package semanticproposal

import (
	"context"
	"testing"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func TestManualReferencePersonRefusesIncompleteAuthority(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	for _, current := range []context.Context{ctx, context.Background()} {
		out, err := reader.prepareNativeInputManualReferencePerson(current, auth.Identity{}, Request{}, sourcePreparationProof{}, registeredNativeInputManualInitialProtection{}, TransferColorProfile{}, v2input.Ref{})
		if err == nil || out.person.evidence != "" || out.source.source.bucket != "" {
			t.Fatal("incomplete authority exposed person reference", err)
		}
		if current == ctx && err != context.Canceled {
			t.Fatal(err)
		}
		tx := &manualLockTx{role: "photo_scheduler"}
		err = reader.lockNativeInputManualReferencePerson(current, tx, auth.Identity{}, Request{}, nativeInputManualReferencePerson{}, TransferColorProfile{}, nil)
		if err == nil || len(tx.calls) != 0 {
			t.Fatal("incomplete person reference reached SQL", err)
		}
		if current == ctx && err != context.Canceled {
			t.Fatal(err)
		}
	}
	root, count, proof, err := reader.verifyNativeInputManualPersonProtectionCoverage(ctx, auth.Identity{}, Request{}, registeredNativeInputManualInitialProtection{}, TransferColorProfile{}, v2input.Ref{}, v2qualification.Expected{})
	if err != context.Canceled || root != (v2input.Ref{}) || count != 0 || proof.evidence != "" {
		t.Fatal("cancelled coverage exposed evidence", err)
	}
}
