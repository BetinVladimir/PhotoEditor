package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestNativeInputDefectSavedLocatorCancellationAndAbsentScope(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	locator, err := (SQLTransferSourceReader{}).readNativeInputDefectSavedLocator(ctx, auth.Identity{}, Request{}, v2input.Ref{}, v2input.Ref{})
	if !errors.Is(err, context.Canceled) || locator.params.ArtifactID != uuid.Nil {
		t.Fatalf("cancelled locator exposed authority: %v", err)
	}
	locator, err = decodeNativeInputDefectSavedLocator([]byte("{}"), "", uuid.Nil, auth.Identity{}, Request{}, v2input.Ref{}, v2input.Ref{})
	if !errors.Is(err, v2input.ErrInvalid) || locator.params.ArtifactID != uuid.Nil {
		t.Fatalf("unscoped locator accepted: %v", err)
	}
}

func TestNativeInputDefectSavedParamsCancelledBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	locator, raw, err := (SQLTransferSourceReader{}).readNativeInputDefectSavedParams(ctx, auth.Identity{}, Request{}, v2input.Ref{}, v2input.Ref{})
	if !errors.Is(err, context.Canceled) || raw != nil || locator.params.ArtifactID != uuid.Nil {
		t.Fatalf("cancelled Params read exposed data: %v", err)
	}
}

func TestNativeInputDefectFaceRestoreCancelledBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	prepared, err := (SQLTransferSourceReader{}).restoreNativeInputDefectFaceParams(ctx, auth.Identity{}, Request{}, v2input.Ref{}, v2input.Ref{}, nativeFaceDraftPlan{}, nativeSkinDraftPlan{}, TransferColorProfile{}, [2]float64{.002, .012}, nil, nil, nil)
	if !errors.Is(err, context.Canceled) || prepared.params.params != "" {
		t.Fatalf("cancelled recovery exposed prepared Params: %v", err)
	}
}

func TestNativeInputDefectPolicyReadCancelledBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	policy, err := (SQLTransferSourceReader{}).readNativeInputSavedDefectPolicy(ctx, auth.Identity{}, Request{}, v2input.Ref{}, v2input.Ref{})
	if !errors.Is(err, context.Canceled) || policy.Permanent != nil || policy.Faces != nil || policy.Protections != nil {
		t.Fatal("cancelled policy read exposed mask declarations", err)
	}
}
