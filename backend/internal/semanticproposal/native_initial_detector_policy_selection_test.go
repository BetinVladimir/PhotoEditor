package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestInitialDetectorControls(t *testing.T) {
	for _, score := range []float64{.5, .8, .99} {
		for _, count := range []int{1, 100} {
			if err := validateInitialDetectorControls(score, count); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, score := range []float64{.49, 1, math.NaN(), math.Inf(1)} {
		if err := validateInitialDetectorControls(score, 20); err == nil {
			t.Fatal("invalid threshold accepted")
		}
	}
	for _, count := range []int{0, 101} {
		if err := validateInitialDetectorControls(.8, count); err == nil {
			t.Fatal("unbounded face count accepted")
		}
	}
}
func TestInitialDetectorSelectionCancelledBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	selected, err := (SQLTransferSourceReader{}).selectInitialDetectorConfiguration(ctx, auth.Identity{}, uuid.New(), TransferPreparation{}, 1)
	if !errors.Is(err, context.Canceled) || selected.Report.ArtifactID != uuid.Nil {
		t.Fatal("cancelled selection exposed report", err)
	}
	plan, ticket, err := (nativePreparationBootstrap{}).bootstrapQualifiedInitialDetector(ctx, auth.Identity{}, nativePreparationAnchor{}, TransferPreparation{}, 1, nil, .8, 20)
	if !errors.Is(err, context.Canceled) || plan.seal != "" || ticket != (v2input.JobTicket{}) {
		t.Fatal("cancelled qualified start exposed receipt", err)
	}
}

func TestQualifiedInitialManualDetectorCancellationBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plan, ticket, err := (nativePreparationBootstrap{}).bootstrapQualifiedInitialManualDetector(ctx, auth.Identity{}, nativePreparationAnchor{}, TransferPreparation{}, 1, nil, .8, 20, nil, [2]float64{})
	if !errors.Is(err, context.Canceled) || plan.seal != "" || ticket != (v2input.JobTicket{}) {
		t.Fatal("cancelled manual qualification exposed receipt", err)
	}
}
