package semanticproposal

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func TestNativeFaceStartRefusesBeforeInfrastructure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, operation := range []string{"face.landmarks", "face.parse", "face.detect"} {
		out, err := (nativePreparationBootstrap{}).bootstrapFace(ctx, auth.Identity{}, detectorDraftPlan{}, TransferColorProfile{}, operation, v2input.Ref{}, nativePreparationBudget{})
		if !errors.Is(err, context.Canceled) || out.plan.seal != "" || out.ticket != (v2input.JobTicket{}) {
			t.Fatal("cancelled start exposed submission", err)
		}
		out, err = (nativePreparationBootstrap{}).bootstrapFace(context.Background(), auth.Identity{}, detectorDraftPlan{}, TransferColorProfile{}, operation, v2input.Ref{}, nativePreparationBudget{})
		if err == nil || out.plan.seal != "" || out.ticket != (v2input.JobTicket{}) {
			t.Fatal("missing dependencies exposed submission")
		}
	}
}
func TestNativeFaceStartBudgetBounds(t *testing.T) {
	valid := nativePreparationBudget{RAM: 1, VRAM: 1, Scratch: 1, Output: 1, Reserved: 8388609}
	if err := validateNativeFaceStartBudget(valid); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*nativePreparationBudget){func(b *nativePreparationBudget) { b.VRAM = 0 }, func(b *nativePreparationBudget) { b.Reserved-- }, func(b *nativePreparationBudget) { b.Output = 9007199254740991 }, func(b *nativePreparationBudget) { b.Costs.RAMBytes = -1 }} {
		b := valid
		mutate(&b)
		if err := validateNativeFaceStartBudget(b); err == nil {
			t.Fatal("invalid capacity accepted")
		}
	}
}
