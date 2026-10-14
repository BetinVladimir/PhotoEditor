package workflow

import (
	"context"
	"errors"
	"testing"

	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

func TestSignedAssemblyProcessorRejectsOffPolicy(t *testing.T) {
	plan, _, _, _, _ := deliveryExportPlanFixture(t, nil)
	mode, err := plan.ProvenanceMode()
	if err != nil || mode != "off" { t.Fatalf("frozen mode: %q %v", mode, err) }
	state, err := (SignedDeliveryAssemblyProcessor{}).ProcessAssembly(context.Background(),
		delivery.ExpansionLease{}, plan)
	if state != "" || !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("off policy entered signed assembly: %q %v", state, err)
	}
	if _, err := (delivery.PreparePlan{}).ProvenanceMode(); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("missing frozen policy accepted: %v", err)
	}
}
