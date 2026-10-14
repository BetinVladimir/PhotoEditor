package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

func TestSchedulerLearningOccurrenceRejectsUninstalledAndWrongParent(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref}
	learning := v2draft.FrozenRenderLearning{VersionID: uuid.New(), Model: ref}
	node := v2draft.LayerRef{Contract: "CandidateProfile", Ref: ref,
		ParentRef:       v2input.Ref{ArtifactID: uuid.New(), SHA256: ref.SHA256},
		ExpectedBinding: &binding, ExpectedLearning: &learning}
	if err := catalog.QualifyExportLearningOccurrence(context.Background(), node, []byte("{}")); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled authority: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catalog.QualifyExportLearningOccurrence(ctx, node, []byte("{}")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled authority: %v", err)
	}
	node.Contract = "DescriptorShard"
	if err := catalog.QualifyExportLearningOccurrence(context.Background(), node, []byte("{}")); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("wrong descriptor lineage: %v", err)
	}
}
