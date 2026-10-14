package semanticproposal

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Locator tests do not claim producer, physical mask or qualification authority.
func TestTransferProtectionSelectionRefusesAmbiguityAndMalformedPins(t *testing.T) {
	candidate := personCandidate{root: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, pixels: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}}
	if got, err := uniqueTransferTargetProtectionCandidate([]personCandidate{candidate}); err != nil || got != candidate {
		t.Fatal("unique original locator refused", err)
	}
	other := candidate
	other.root.ArtifactID = uuid.New()
	missing := candidate
	missing.root.ArtifactID = uuid.Nil
	invalidSHA := candidate
	invalidSHA.pixels.SHA256 = "invalid"
	self := candidate
	self.pixels = candidate.root
	for _, values := range [][]personCandidate{nil, {candidate, other}, {candidate, candidate}, {missing}, {invalidSHA}, {self}} {
		got, err := uniqueTransferTargetProtectionCandidate(values)
		if err == nil || got != (personCandidate{}) {
			t.Fatal("ambiguous/malformed selection exposed pins", err)
		}
		if len(values) == 0 {
			if !errors.Is(err, errTransferProtectionUnavailable) || !errors.Is(err, v2qualification.ErrUnqualified) {
				t.Fatal("absent producer is not unavailable", err)
			}
		} else if errors.Is(err, errTransferProtectionUnavailable) {
			t.Fatal("invalid existing producer eligible for regeneration", err)
		}
	}
}

func TestTransferTargetSelectionCancelledBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	if got, err := reader.transferTargetProtectionCandidate(ctx, auth.Identity{}, uuid.New(), v2input.SessionVersionBinding{}); err != context.Canceled || got != (personCandidate{}) {
		t.Fatal("cancelled candidate reached IO", err)
	}
	if got, err := reader.selectTransferTargetProtectionConfiguration(ctx, auth.Identity{}, Request{}, PublishedIntent{}, TransferPreparation{}); err != context.Canceled || got.TargetProtection != nil {
		t.Fatal("cancelled target selection exposed configuration", err)
	}
}
