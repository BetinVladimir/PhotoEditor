package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2qualification"
	"testing"
)

func TestInitialFaceBudgetVerifiedMetrics(t *testing.T) {
	f := nativeRecoveryFactoryCapacityFixture()
	metrics := v2qualification.Metrics{PeakRSSBytes: 1.1, PeakVRAMBytes: 2.1, ScratchBytes: 3.1}
	budget, err := initialNativeFaceBudget(f.Capacity, f.ReservedBytes, metrics)
	if err != nil || budget.Costs.RAMBytes != 2 || budget.Costs.VRAMBytes != 3 || budget.Costs.ScratchBytes != 4 || budget.Output != f.Capacity.Output {
		t.Fatal("verified peaks were not conservatively bounded", err)
	}
	for _, bad := range []v2qualification.Metrics{{PeakRSSBytes: -1}, {PeakVRAMBytes: math.NaN()}, {ScratchBytes: math.Inf(1)}, {PeakVRAMBytes: float64(f.Capacity.VRAM) + 1}, {PeakRSSBytes: 9007199254740992}} {
		got, err := initialNativeFaceBudget(f.Capacity, f.ReservedBytes, bad)
		if err == nil || got.RAM != 0 {
			t.Fatal("invalid or excessive measurements granted budget")
		}
	}
	if _, err := initialNativeFaceBudget(f.Capacity, 1, metrics); err == nil {
		t.Fatal("insufficient transport reservation accepted")
	}
}
func TestInitialFacePolicySelectionCancelledBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	selected, err := (SQLTransferSourceReader{}).selectInitialFacePipelineConfiguration(ctx, auth.Identity{}, uuid.New(), nativeRecoveryFactoryCapacityFixture().Capacity, 1, nil)
	if !errors.Is(err, context.Canceled) || selected.Landmarks.Report.ArtifactID != uuid.Nil || selected.TransportDocuments != nil {
		t.Fatal("cancelled selection exposed report authority", err)
	}
	out, err := (nativePreparationBootstrap{}).advanceQualifiedInitialFacePipeline(ctx, auth.Identity{}, nativePreparationAnchor{}, TransferPreparation{}, 1, nil)
	if !errors.Is(err, context.Canceled) || out.phase != "" {
		t.Fatal("cancelled automatic pipeline exposed progress", err)
	}
}
