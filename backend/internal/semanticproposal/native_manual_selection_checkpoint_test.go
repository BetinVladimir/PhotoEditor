package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestManualCheckpointBounds(t *testing.T) {
	sha := strings.Repeat("a", 64)
	value := nativeManualSelectionCheckpoint{Schema: 1, Parent: uuid.New(), Project: uuid.New(), Actor: uuid.New(), Session: uuid.New(), RequestSHA: sha, Source: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, CanvasHash: sha}, Pixels: []v2input.Ref{{ArtifactID: uuid.New(), SHA256: sha}}, Diameter: [2]float64{.002, .012}}
	if err := validateNativeManualSelection(value); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*nativeManualSelectionCheckpoint){func(v *nativeManualSelectionCheckpoint) { v.Schema = 2 }, func(v *nativeManualSelectionCheckpoint) { v.Session = uuid.Nil }, func(v *nativeManualSelectionCheckpoint) { v.RequestSHA = "bad" }, func(v *nativeManualSelectionCheckpoint) { v.Pixels = nil }, func(v *nativeManualSelectionCheckpoint) { v.Diameter[0] = math.NaN() }, func(v *nativeManualSelectionCheckpoint) { v.Diameter = [2]float64{.1, .01} }} {
		changed := value
		mutate(&changed)
		if err := validateNativeManualSelection(changed); err == nil {
			t.Fatal("invalid checkpoint accepted")
		}
	}
}
func TestManualCheckpointCancellationBeforeStore(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	if err := reader.saveNativeManualSelection(ctx, nil, auth.Identity{}, detectorDraftPlan{}, nil, [2]float64{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled save accessed SQL", err)
	}
	value, err := reader.readNativeManualSelection(ctx, auth.Identity{}, detectorDraftPlan{})
	if !errors.Is(err, context.Canceled) || value.Parent != uuid.Nil {
		t.Fatal("cancelled read exposed checkpoint", err)
	}
}
