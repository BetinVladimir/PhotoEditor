package delivery

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

func TestDelegatedRenderClosureResolverRequiresScopeAndRoots(t *testing.T) {
	resolver := DelegatedRenderClosureResolver{}
	if _, err := resolver.ResolveDelegatedTaskInputEvidence(context.Background(),
		provenance.CanonicalSigningTask{}, nil, v2input.SessionVersionBinding{}); err == nil {
		t.Fatal("unconfigured delegated closure accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := resolver.ResolveDelegatedTaskInputEvidence(ctx,
		provenance.CanonicalSigningTask{}, nil, v2input.SessionVersionBinding{}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
}

func TestDelegatedRenderInputObjectCatalogRejectsUnregisteredOccurrence(t *testing.T) {
	catalog := DelegatedRenderInputObjectCatalog{}
	if _, err := catalog.Lookup(context.Background(), auth.Identity{}, uuid.New(),
		v2draft.LayerRef{Ref: v2input.Ref{ArtifactID: uuid.New()}, Contract: "WorkingImage"}); err == nil {
		t.Fatal("unregistered occurrence accepted")
	}
}

func TestDelegatedRegisteredOccurrencePreservesTypedAlias(t *testing.T) {
	parent := v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	child := v2input.Ref{ArtifactID: uuid.New(), SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	inventory := []v2input.Entry{{Ref: parent, Contract: "ImageReport"}, {Ref: child, Contract: "NpyArray"}}
	node := v2draft.LayerRef{Ref: child, ParentRef: parent, Contract: "Artifact"}
	if contract, ok := delegatedRegisteredOccurrence(inventory, node, "Artifact"); !ok || contract != "NpyArray" {
		t.Fatal("typed registered input not accepted as generic occurrence")
	}
	node.ParentRef = v2input.Ref{ArtifactID: uuid.New(), SHA256: parent.SHA256}
	if _, ok := delegatedRegisteredOccurrence(inventory, node, "Artifact"); ok { t.Fatal("foreign parent accepted") }
	node.ParentRef = parent
	node.Contract = "WorkingImage"
	if _, ok := delegatedRegisteredOccurrence(inventory, node, "WorkingImage"); ok { t.Fatal("wrong typed contract accepted") }
}

func TestDelegatedRenderSpecialGuardUsesRegisteredContract(t *testing.T) {
	for _, contract := range []string{"CandidateProfile", "DescriptorShard", "StyleBackgroundAsset"} {
		if delegatedRenderLiveGuard(contract) == "" { t.Fatal("special contract has no live guard", contract) }
	}
	if delegatedRenderLiveGuard("Artifact") != "" { t.Fatal("generic contract selected a special guard") }
}
