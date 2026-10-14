package semanticproposal

import (
	"context"
	"errors"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestNativeInputTransferPreparationCancelledReturnsNoBatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	inputs, documents, err := reader.prepareNativeInputTransferInputs(ctx, auth.Identity{}, Request{}, PublishedIntent{}, TransferPreparation{}, nativeInputTransferReferenceDocuments{})
	if !errors.Is(err, context.Canceled) || inputs != nil || documents != nil {
		t.Fatalf("cancelled preparation exposed batch: %v", err)
	}
	err = reader.validateNativeInputTransferReference(ctx, auth.Identity{}, Request{}, TransferPreparation{}, nativeInputTransferReferenceDocuments{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled validation: %v", err)
	}
	err = reader.validateNativeInputTransferReference(context.Background(), auth.Identity{}, Request{}, TransferPreparation{}, nativeInputTransferReferenceDocuments{})
	if !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("missing independent person policy accepted: %v", err)
	}
}
