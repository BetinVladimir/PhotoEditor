package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestAdoptedLensBindingRejectsMismatchedRootAndMalformedEvidence(t *testing.T) {
	_, base, root := compositionInputs()
	binding := AdoptedLensBinding{ResultID: uuid.New(), Result: root, Base: base}
	if err := validateAdoptedLensBinding(binding, root, base.AssetVersionID); err != nil {
		t.Fatal(err)
	}
	cases := []func(*AdoptedLensBinding){
		func(b *AdoptedLensBinding) { b.ResultID = uuid.Nil },
		func(b *AdoptedLensBinding) { b.Result.ArtifactID = uuid.New() },
		func(b *AdoptedLensBinding) { b.Base.AssetVersionID = uuid.New() },
		func(b *AdoptedLensBinding) { b.Base.Source = v2input.Ref{} },
		func(b *AdoptedLensBinding) { b.Base.CanvasHash = "invalid" },
	}
	for i, mutate := range cases {
		bad := binding
		mutate(&bad)
		if err := validateAdoptedLensBinding(bad, root, base.AssetVersionID); err == nil {
			t.Fatalf("accepted %d", i)
		}
	}
}

func TestAdoptedLensSourceRequiresConfiguredScopedDatabase(t *testing.T) {
	_, base, root := compositionInputs()
	source := SQLAdoptedLensSource{}
	binding, err := source.Load(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), base.AssetVersionID, uuid.New(), root)
	if err == nil || binding.ResultID != uuid.Nil {
		t.Fatal("unconfigured source returned proof")
	}
}
