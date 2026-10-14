package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestNativeInputTransferReconstructionRejectsBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := TransferDraftPreparer{}
	plan, err := p.reconstructNativeInputTransferPlan(ctx, auth.Identity{}, Request{}, nativeInputReferencePerson{}, PublishedIntent{}, v2input.Ref{}, nil, nil)
	if !errors.Is(err, context.Canceled) || len(plan.core.tasks) != 0 {
		t.Fatalf("cancelled reconstruction: %v", err)
	}
	plan, err = p.reconstructNativeInputTransferPlan(context.Background(), auth.Identity{}, Request{}, nativeInputReferencePerson{}, PublishedIntent{}, v2input.Ref{}, nil, nil)
	if !errors.Is(err, v2input.ErrInvalid) || len(plan.core.tasks) != 0 {
		t.Fatalf("missing original metadata: %v", err)
	}
	ticket, err := p.reconstructAndResumeNativeInputTransfer(context.Background(), auth.Identity{}, uuid.Nil, "", nil, nativeInputReferencePerson{}, v2input.Built{}, PublishedIntent{}, v2input.Ref{}, nil, nil)
	if !errors.Is(err, v2input.ErrInvalid) || ticket.RunID != uuid.Nil {
		t.Fatalf("missing recovery dependencies: %v", err)
	}
}

func TestNativeInputTransferLoadResumeCancelledBeforeReadingSavedData(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := TransferDraftPreparer{}
	ticket, err := p.loadAndResumeNativeInputTransfer(ctx, auth.Identity{}, uuid.Nil, "", nil, nativeInputReferencePerson{}, v2input.Built{})
	if !errors.Is(err, context.Canceled) || ticket.RunID != uuid.Nil {
		t.Fatalf("cancelled saved load: %v", err)
	}
	ticket, err = p.loadAndResumeNativeInputTransfer(context.Background(), auth.Identity{}, uuid.Nil, "", nil, nativeInputReferencePerson{}, v2input.Built{})
	if !errors.Is(err, v2input.ErrInvalid) || ticket.RunID != uuid.Nil {
		t.Fatalf("missing saved loader dependencies: %v", err)
	}
}

func TestNativeInputOriginalTransferRecoveryRefusesBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := TransferDraftPreparer{}
	ticket, err := p.restoreAndResumeNativeInputTransfer(ctx, nil, auth.Identity{}, uuid.Nil, "", nil, PublishedIntent{}, v2input.Ref{}, nil, [2]float64{.002, .012}, nil, nil, nil, nil)
	if !errors.Is(err, context.Canceled) || ticket.RunID != uuid.Nil {
		t.Fatal("cancelled original recovery exposed ticket", err)
	}
	ticket, err = p.restoreAndResumeNativeInputTransfer(context.Background(), nil, auth.Identity{}, uuid.Nil, "", nil, PublishedIntent{}, v2input.Ref{}, nil, [2]float64{.002, .012}, nil, nil, nil, nil)
	if !errors.Is(err, v2input.ErrInvalid) || ticket.RunID != uuid.Nil {
		t.Fatal("incomplete original recovery reached IO", err)
	}
	built, err := recoverNativeInputDefectTransport(ctx, nil, auth.Identity{}, nativeDefectDraftPlan{})
	if !errors.Is(err, context.Canceled) || built.Manifest.ArtifactID != uuid.Nil {
		t.Fatal("cancelled original defect transport exposed bindings", err)
	}
}
