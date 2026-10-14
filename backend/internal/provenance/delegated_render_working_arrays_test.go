package provenance

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

func TestDelegatedRenderWorkingArraysRequireWitnessedParent(t *testing.T) {
	parent := v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	evidence := v2draft.TaskInputEvidence{Occurrences: []v2draft.OccurrenceWitness{{
		Node: v2draft.LayerRef{Contract: "WorkingPixels", ParentRef: parent},
	}}}
	if err := verifyDelegatedRenderWorkingArrays(context.Background(), nil, nil,
		uuid.New(), evidence); err == nil {
		t.Fatal("array without witnessed WorkingImage accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifyDelegatedRenderWorkingArrays(ctx, nil, nil,
		uuid.New(), evidence); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
}
