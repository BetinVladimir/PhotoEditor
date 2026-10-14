package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestNativeInputManualTransferRequestRejectsBeforePublication(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := TransferDraftPreparer{}
	ticket, err := p.prepareAndActivateNativeInputManualTransfer(ctx, auth.Identity{}, uuid.Nil, "", nil, nativeInputManualReferencePerson{}, v2input.Built{})
	if !errors.Is(err, context.Canceled) || ticket.RunID != uuid.Nil {
		t.Fatalf("cancelled orchestration: %v", err)
	}
	ticket, err = p.prepareAndActivateNativeInputManualTransfer(context.Background(), auth.Identity{}, uuid.Nil, "", nil, nativeInputManualReferencePerson{}, v2input.Built{})
	if !errors.Is(err, v2input.ErrInvalid) || ticket.RunID != uuid.Nil {
		t.Fatalf("missing dependencies accepted: %v", err)
	}
}
