package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestNativeInputManualTransferReconstructionRejectsBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := TransferDraftPreparer{}
	plan, err := p.reconstructNativeInputManualTransferPlan(ctx, auth.Identity{}, Request{}, nativeInputManualReferencePerson{}, PublishedIntent{}, v2input.Ref{}, nil, nil)
	if !errors.Is(err, context.Canceled) || len(plan.core.tasks) != 0 {
		t.Fatalf("cancelled reconstruction: %v", err)
	}
	plan, err = p.reconstructNativeInputManualTransferPlan(context.Background(), auth.Identity{}, Request{}, nativeInputManualReferencePerson{}, PublishedIntent{}, v2input.Ref{}, nil, nil)
	if !errors.Is(err, v2input.ErrInvalid) || len(plan.core.tasks) != 0 {
		t.Fatalf("missing original metadata: %v", err)
	}
	ticket, err := p.reconstructAndResumeNativeInputManualTransfer(context.Background(), auth.Identity{}, uuid.Nil, "", nil, nativeInputManualReferencePerson{}, v2input.Built{}, PublishedIntent{}, v2input.Ref{}, nil, nil)
	if !errors.Is(err, v2input.ErrInvalid) || ticket.RunID != uuid.Nil {
		t.Fatalf("missing recovery dependencies: %v", err)
	}
}

func TestNativeInputManualTransferLoadResumeCancelledBeforeReadingSavedData(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := TransferDraftPreparer{}
	ticket, err := p.loadAndResumeNativeInputManualTransfer(ctx, auth.Identity{}, uuid.Nil, "", nil, nativeInputManualReferencePerson{}, v2input.Built{})
	if !errors.Is(err, context.Canceled) || ticket.RunID != uuid.Nil {
		t.Fatalf("cancelled saved load: %v", err)
	}
	ticket, err = p.loadAndResumeNativeInputManualTransfer(context.Background(), auth.Identity{}, uuid.Nil, "", nil, nativeInputManualReferencePerson{}, v2input.Built{})
	if !errors.Is(err, v2input.ErrInvalid) || ticket.RunID != uuid.Nil {
		t.Fatalf("missing saved loader dependencies: %v", err)
	}
}
