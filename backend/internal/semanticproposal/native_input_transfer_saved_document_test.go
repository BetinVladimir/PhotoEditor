package semanticproposal

import (
	"context"
	"errors"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestNativeInputSavedSnapshotRejectsCancelledBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	raw, err := reader.readNativeInputTransferSavedSnapshot(ctx, auth.Identity{}, Request{}, nativeInputTransferSavedMetadata{}, 1024)
	if !errors.Is(err, context.Canceled) || raw != nil {
		t.Fatalf("cancelled snapshot exposed data: %v", err)
	}
	_, err = reader.nativeInputTransferSavedDocumentObject(context.Background(), auth.Identity{}, Request{}, 5000, v2input.Ref{})
	if !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("invalid document ordinal accepted: %v", err)
	}
}
