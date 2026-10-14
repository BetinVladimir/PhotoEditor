package semanticproposal

import (
	"context"
	"photoeditor/backend/internal/auth"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func nativeRecoveryFactoryCapacityFixture() SQLNativeParentRecoveryFactory {
	limits := ManualRasterVerificationLimits{PointChecks: 1000, EdgeEvaluations: 1000, PayloadBytes: 1 << 20}
	return SQLNativeParentRecoveryFactory{Capacity: TransferPreparation{Color: TransferColorProfile{sha: strings.Repeat("a", 64)}, RAM: 1 << 30, VRAM: 1 << 30, Scratch: 1 << 30, Output: 1 << 20, MetadataBytes: 16 << 20}, ReservedBytes: 16 << 20, Timeout: time.Minute, ManualLimits: limits, ManualBatchLimits: limits}
}

func TestNativeRecoveryFactoryRejectsUnboundedCapacity(t *testing.T) {
	valid := nativeRecoveryFactoryCapacityFixture()
	if err := valid.validateCapacity(); err != nil {
		t.Fatal("bounded capacity refused", err)
	}
	for _, mode := range []string{"reservation", "timeout", "manual_points", "manual_edges", "batch_bytes", "metadata", "profile"} {
		changed := valid
		switch mode {
		case "reservation":
			changed.ReservedBytes = 0
		case "timeout":
			changed.Timeout = 30 * time.Minute
		case "manual_points":
			changed.ManualLimits.PointChecks = 0
		case "manual_edges":
			changed.ManualLimits.EdgeEvaluations = -1
		case "batch_bytes":
			changed.ManualBatchLimits.PayloadBytes = 9007199254740992
		case "metadata":
			changed.Capacity.MetadataBytes = changed.Capacity.RAM
		case "profile":
			changed.Capacity.Color = TransferColorProfile{}
		}
		if err := changed.validateCapacity(); err == nil {
			t.Fatal("unbounded runtime capacity accepted", mode)
		}
	}
}

func TestNativeRecoveryFactoryCancellationAndAbsentResourcesBeforeSQL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	factory := nativeRecoveryFactoryCapacityFixture()
	if job, err := factory.ForProject(ctx, uuid.New()); err != context.Canceled || job.Preparer.Reader.DB != nil {
		t.Fatal("cancelled factory reached SQL", err)
	}
	if job, err := factory.ForProject(context.Background(), uuid.New()); err == nil || job.CatalogForReference != nil {
		t.Fatal("absent role pools/storage produced runtime")
	}
}

func TestManualPreparationFactoryCancellationBeforeCatalog(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	worked, err := nativeRecoveryFactoryCapacityFixture().StartManualPreparation(ctx, auth.Identity{}, uuid.New(), "", nil, .8, 20, nil, [2]float64{})
	if worked || err != context.Canceled {
		t.Fatal("cancelled preparation factory accessed source catalog", err)
	}
}
