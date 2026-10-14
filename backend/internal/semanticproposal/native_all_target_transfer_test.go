package semanticproposal

import (
	"context"
	"errors"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"testing"
)

func TestNativeAllTargetTransferCancelledReturnsNoAuthority(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := SQLTransferSourceReader{}
	inputs, docs, err := s.prepareNativeAllTargetTransferInputs(ctx, auth.Identity{}, Request{}, PublishedIntent{}, TransferPreparation{}, nativeInputTransferReferenceDocuments{}, nil)
	if !errors.Is(err, context.Canceled) || inputs != nil || docs != nil {
		t.Fatal("cancelled input preparation", err)
	}
	plan, err := s.compileNativeAllTargetTransferWorkflowPlan(ctx, auth.Identity{}, Request{}, PublishedIntent{}, v2input.Ref{}, nil, nil, nil, nil, TransferPreparation{})
	if !errors.Is(err, context.Canceled) || plan.core.run != (Request{}).RunID {
		t.Fatal("cancelled compiler", err)
	}
	proofs, err := (TransferDraftPreparer{}).restoreNativeTransferTargets(ctx, auth.Identity{}, detectorDraftPlan{}, nativeTargetPreparationManifest{}, nativeInputTransferReferenceDocuments{}, nil)
	if !errors.Is(err, context.Canceled) || proofs != nil {
		t.Fatal("cancelled target restore", err)
	}
}
func TestNativeAllTargetCoreRefusesGenericAdmission(t *testing.T) {
	core := WorkflowPlan{nativeTargetsRequired: true}
	if err := validateNativeTransferPlanScope(auth.Identity{}, Request{}, core); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("generic scope accepted native targets", err)
	}
	s := SQLTransferSourceReader{}
	if err := s.validateNativeInputTransferPlanScope(context.Background(), auth.Identity{}, Request{}, nativeInputTransferWorkflowPlan{core: core}, TransferPreparation{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("old native scope accepted native targets", err)
	}
	if err := s.validateNativeAllTargetTransferPlanScope(context.Background(), auth.Identity{}, Request{}, nativeAllTargetTransferWorkflowPlan{core: core}, TransferPreparation{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("incomplete native target plan accepted", err)
	}
}

func TestOriginalPinnedReferenceCannotUseLegacyAdmission(t *testing.T) {
	s := SQLTransferSourceReader{}
	reference := nativeInputReferencePerson{originalPinned: true}
	if err := s.lockNativeInputReferencePerson(context.Background(), nil, auth.Identity{}, Request{}, reference, TransferColorProfile{}, nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("legacy reference lock accepted pinned mode", err)
	}
	plan := nativeInputTransferWorkflowPlan{reference: nativeInputTransferReferenceDocuments{person: reference}}
	if err := s.validateNativeInputTransferPlanScope(context.Background(), auth.Identity{}, Request{}, plan, TransferPreparation{}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("legacy scope accepted pinned mode", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.nativeInputCoverageCandidate(ctx, auth.Identity{}, Request{}, registeredNativeInputProtection{}, v2input.Ref{}, v2qualification.Expected{}, true); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled original selector", err)
	}
	if _, err := s.nativeInputCoverageCandidate(context.Background(), auth.Identity{}, Request{}, registeredNativeInputProtection{}, v2input.Ref{}, v2qualification.Expected{}, true); err == nil {
		t.Fatal("unproven original producer accepted")
	}
}

func TestNativeAllTargetPersistedConfigRejectsMissingAuthority(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := SQLTransferSourceReader{}
	if c, err := s.restoreNativeAllTargetTransferCapacity(ctx, auth.Identity{}, Request{}, nil, "", nativeInputTransferReferenceDocuments{}, TransferColorProfile{}); !errors.Is(err, context.Canceled) || c.TargetProtection != nil {
		t.Fatal("cancelled capacity restore", err)
	}
	for _, raw := range [][]byte{nil, []byte("{}"), []byte("{")} {
		if _, err := decodeNativeAllTargetTransferConfig(raw, ""); err == nil {
			t.Fatal("incomplete persisted config accepted")
		}
	}
	if _, _, err := compileNativeAllTargetTransferConfig(ctx, auth.Identity{}, Request{}, WorkflowPlan{}, nativeInputTransferReferenceDocuments{}, nil, TransferPreparation{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled config compiler", err)
	}
}

func TestNativeOriginalReferenceLockRequiresPreparedAuthority(t *testing.T) {
	s := SQLTransferSourceReader{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.lockNativeAllTargetTransferReference(ctx, nil, auth.Identity{}, Request{}, nativeInputTransferReferenceDocuments{}, TransferPreparation{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled original reference reached SQL", err)
	}
	if err := s.lockNativeAllTargetTransferReference(context.Background(), nil, auth.Identity{}, Request{}, nativeInputTransferReferenceDocuments{}, TransferPreparation{}, nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("missing original reference authority accepted", err)
	}
}
