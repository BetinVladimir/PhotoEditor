package workflow

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/delivery"
)

func TestDeliverySigningStageRequiresInstalledClosure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (DeliverySigningStage{}).ProcessSigning(ctx,
		delivery.ExpansionLease{}, delivery.PreparePlan{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled stage accepted", err)
	}
	if _, err := (DeliverySigningStage{}).ProcessSigning(context.Background(),
		delivery.ExpansionLease{}, delivery.PreparePlan{}); err == nil {
		t.Fatal("unconfigured stage accepted")
	}
}
