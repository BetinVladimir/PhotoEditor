package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"testing"
)

func TestAuthoredFaceQualificationCancellationPrecedesDependencies(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (SQLAuthoredFaceQualification{}).Read(ctx, auth.Identity{}, uuid.Nil, uuid.Nil, v2qualification.Expected{}, v2input.Ref{})
	if !errors.Is(err, context.Canceled) {
		t.Fatal("lost cancellation", err)
	}
}
func TestAuthoredFaceMeasuredCostsRequireActualPositiveFiniteGPUObservation(t *testing.T) {
	good := v2qualification.Metrics{PeakRSSBytes: 1.1, PeakVRAMBytes: 2.2, ScratchBytes: 3.3}
	cost, err := authoredFaceMeasuredCosts(good)
	if err != nil || cost.RAMBytes != 2 || cost.VRAMBytes != 3 || cost.ScratchBytes != 4 {
		t.Fatal(cost, err)
	}
	for _, bad := range []float64{0, -1, math.NaN(), math.Inf(1), 9007199254740992} {
		m := good
		m.PeakVRAMBytes = bad
		if _, err := authoredFaceMeasuredCosts(m); err == nil {
			t.Fatal("invalid model observation", bad)
		}
	}
}
