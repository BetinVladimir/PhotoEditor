package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2qualification"
	"testing"
)

func TestInitialDefectCPUResourceBudget(t *testing.T) {
	f := nativeRecoveryFactoryCapacityFixture()
	b, err := initialNativeDefectBudget(f.Capacity, f.ReservedBytes, v2qualification.Metrics{PeakRSSBytes: 1.1, ScratchBytes: 2.1})
	if err != nil || b.VRAM != 0 || b.Costs.VRAMBytes != 0 || b.Costs.RAMBytes != 2 || b.Costs.ScratchBytes != 3 {
		t.Fatal("CPU qualification did not produce bounded CPU budget", err)
	}
	b, err = initialNativeDefectBudget(f.Capacity, f.ReservedBytes, v2qualification.Metrics{PeakVRAMBytes: .1})
	if err == nil || b.RAM != 0 {
		t.Fatal("CPU stage accepted GPU model costs")
	}
}
func TestInitialDefectSelectionCancelledBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, err := (SQLTransferSourceReader{}).selectInitialDefectConfiguration(ctx, auth.Identity{}, uuid.New(), TransferPreparation{}, 1)
	if !errors.Is(err, context.Canceled) || c.Report.ArtifactID != uuid.Nil {
		t.Fatal("cancelled defect qualification exposed report", err)
	}
	out, err := (nativePreparationBootstrap{}).bootstrapQualifiedManualDefect(ctx, auth.Identity{}, nativeInputRestoredFaceProducers{}, TransferColorProfile{}, nil, [2]float64{}, TransferPreparation{}, 1, nil)
	if !errors.Is(err, context.Canceled) || out.plan.seal != "" {
		t.Fatal("cancelled qualified manual start exposed plan", err)
	}
}
