package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestNativeInputTransferRequestRejectsBeforePublication(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := TransferDraftPreparer{}
	ticket, err := p.prepareAndActivateNativeInputTransfer(ctx, auth.Identity{}, uuid.Nil, "", nil, nativeInputReferencePerson{}, v2input.Built{})
	if !errors.Is(err, context.Canceled) || ticket.RunID != uuid.Nil {
		t.Fatalf("cancelled orchestration: %v", err)
	}
	ticket, err = p.prepareAndActivateNativeInputTransfer(context.Background(), auth.Identity{}, uuid.Nil, "", nil, nativeInputReferencePerson{}, v2input.Built{})
	if !errors.Is(err, v2input.ErrInvalid) || ticket.RunID != uuid.Nil {
		t.Fatalf("missing dependencies accepted: %v", err)
	}
}
