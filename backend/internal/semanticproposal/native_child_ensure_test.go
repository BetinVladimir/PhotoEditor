package semanticproposal

import (
	"context"
	"errors"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestEnsureNativeChildrenCancelledBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	face, err := (nativePreparationBootstrap{}).ensureFace(ctx, auth.Identity{}, detectorDraftPlan{}, TransferColorProfile{}, "face.parse", v2input.Ref{}, nativePreparationBudget{})
	if !errors.Is(err, context.Canceled) || face.plan.seal != "" || face.ticket != (v2input.JobTicket{}) {
		t.Fatal("cancelled face ensure exposed child", err)
	}
	skin, err := (nativePreparationBootstrap{}).ensureSkin(ctx, auth.Identity{}, nativeFaceDraftPlan{}, TransferColorProfile{}, nativePreparationBudget{})
	if !errors.Is(err, context.Canceled) || skin.plan.seal != "" || skin.ticket != (v2input.JobTicket{}) {
		t.Fatal("cancelled skin ensure exposed child", err)
	}
}
