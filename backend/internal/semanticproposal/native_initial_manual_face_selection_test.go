package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"strings"
	"testing"
)

func TestInitialManualBindingsAreUnambiguousAndOwned(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	faces := []review.DefectProposalFaceInput{{ObjectID: &a}, {ObjectID: &b}}
	target := review.ProtectionMask{Artifact: review.ArtifactRef{ID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Selector: "manual"}
	got, err := bindInitialManualFaceInputs(context.Background(), faces, []review.ProtectionMask{target})
	if err != nil || len(got) != 2 || got[0].Manual == nil || got[1].Manual == nil || got[0].Manual == got[1].Manual || faces[0].Manual != nil {
		t.Fatal("global original target binding lost ownership", err)
	}
	got[0].Manual.Selector = "changed"
	if got[1].Manual.Selector != "manual" || target.Selector != "manual" {
		t.Fatal("binding aliases other inputs")
	}
	specific := target
	specific.Artifact.ID = uuid.New()
	specific.ObjectID = &a
	if _, err := bindInitialManualFaceInputs(context.Background(), faces, []review.ProtectionMask{target, specific}); err == nil {
		t.Fatal("overlapping manual candidates accepted")
	}
	foreign := uuid.New()
	specific.ObjectID = &foreign
	if _, err := bindInitialManualFaceInputs(context.Background(), faces, []review.ProtectionMask{specific}); err == nil {
		t.Fatal("unused foreign face target accepted")
	}
	if _, err := bindInitialManualFaceInputs(context.Background(), faces, []review.ProtectionMask{target, target}); err == nil {
		t.Fatal("duplicate manual target accepted")
	}
}
func TestInitialManualBindingCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := bindInitialManualFaceInputs(ctx, nil, nil)
	if !errors.Is(err, context.Canceled) || got != nil {
		t.Fatal("cancelled binding exposed face inputs", err)
	}
}
