package workflow

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
)

func TestApplyCompositionQualityRequiresExactAdoptedTarget(t *testing.T) {
	item := uuid.New()
	artifact := styleArtifact{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	// Crop and outpaint both arrive through the decided composition review,
	// independent of the operation that produced the candidate.
	for _, operation := range []string{"geometry.crop", "image.outpaint"} {
		t.Run(operation, func(t *testing.T) {
			for _, route := range []string{"candidate", "base", "skip"} {
				outputs := []applyOutput{{ItemID: item, Artifact: artifact, Route: route}}
				if err := validateApplyCompositionQualityTarget(item, artifact.ref(), outputs, 1); err != nil {
					t.Fatal(err)
				}
				if err := validateApplyCompositionQualityTarget(item, review.ArtifactRef{ID: artifact.ArtifactID, SHA256: strings.Repeat("b", 64)}, outputs, 1); err == nil {
					t.Fatal("unadopted document accepted")
				}
				if err := validateApplyCompositionQualityTarget(uuid.New(), artifact.ref(), outputs, 1); err == nil {
					t.Fatal("foreign item accepted")
				}
				if err := validateApplyCompositionQualityTarget(item, artifact.ref(), append(outputs, outputs[0]), 2); err == nil {
					t.Fatal("duplicate item accepted")
				}
				outputs[0].Route = "repair"
				if err := validateApplyCompositionQualityTarget(item, artifact.ref(), outputs, 1); err == nil {
					t.Fatal("non-composition route accepted")
				}
			}
		})
	}
}
