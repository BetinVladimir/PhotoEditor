package semanticproposal

import (
	"context"
	"errors"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"testing"
)

func TestNativeInputPersonPixelsCancelledBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root, count, proof, err := (SQLTransferSourceReader{}).verifyNativeInputPersonProtectionCoverage(ctx, auth.Identity{}, Request{}, registeredNativeInputProtection{}, TransferColorProfile{}, v2input.Ref{}, v2qualification.Expected{})
	if !errors.Is(err, context.Canceled) || root != (v2input.Ref{}) || count != 0 || proof.evidence != "" {
		t.Fatalf("cancelled coverage exposed authority: %v", err)
	}
	err = (SQLTransferSourceReader{}).lockNativeInputPersonEvidence(ctx, nil, auth.Identity{}, Request{}, nativeInputPersonProtectionProof{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled lock: %v", err)
	}
	err = (SQLTransferSourceReader{}).lockNativeInputPersonEvidence(context.Background(), nil, auth.Identity{}, Request{}, nativeInputPersonProtectionProof{})
	if !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("missing tx accepted: %v", err)
	}
}
