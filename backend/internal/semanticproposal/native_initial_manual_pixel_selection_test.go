package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestInitialManualPixelSelectionBounds(t *testing.T) {
	valid := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	if err := validateInitialManualPixelSelection([]v2input.Ref{valid}); err != nil {
		t.Fatal(err)
	}
	for _, refs := range [][]v2input.Ref{nil, {}, {{}}, {{ArtifactID: valid.ArtifactID, SHA256: "bad"}}, {valid, valid}, make([]v2input.Ref, 65)} {
		if err := validateInitialManualPixelSelection(refs); err == nil {
			t.Fatal("invalid manual selection accepted")
		}
	}
}
func TestInitialManualPixelSelectionCancelledBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	faces, err := (SQLTransferSourceReader{}).selectInitialManualFacesFromPixels(ctx, auth.Identity{}, nativeInputRestoredFaceProducers{}, TransferColorProfile{}, nil)
	if !errors.Is(err, context.Canceled) || faces != nil {
		t.Fatal("cancelled selection exposed faces", err)
	}
}
