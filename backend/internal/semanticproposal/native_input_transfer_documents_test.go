package semanticproposal

import (
	"context"
	"errors"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestNativeInputTransferDocumentsRejectBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	docs, err := reader.readNativeInputTransferReferenceDocuments(ctx, auth.Identity{}, Request{}, nativeInputReferencePerson{}, TransferColorProfile{}, 1024)
	if !errors.Is(err, context.Canceled) || docs.reference != nil || docs.selected != nil || docs.protection != nil {
		t.Fatalf("cancelled read exposed documents: %v", err)
	}
	for _, budget := range []int64{0, -1, 9007199254740992} {
		docs, err := reader.readNativeInputTransferReferenceDocuments(context.Background(), auth.Identity{}, Request{}, nativeInputReferencePerson{}, TransferColorProfile{}, budget)
		if !errors.Is(err, v2input.ErrInvalid) || docs.reference != nil || docs.selected != nil || docs.protection != nil {
			t.Fatalf("invalid budget exposed documents: %v", err)
		}
	}
	err = reader.lockNativeInputTransferReference(ctx, nil, auth.Identity{}, Request{}, nativeInputTransferReferenceDocuments{}, TransferColorProfile{}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lock cancellation: %v", err)
	}
	err = reader.lockNativeInputTransferReference(context.Background(), nil, auth.Identity{}, Request{}, nativeInputTransferReferenceDocuments{}, TransferColorProfile{}, nil)
	if !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("missing transaction: %v", err)
	}
}
