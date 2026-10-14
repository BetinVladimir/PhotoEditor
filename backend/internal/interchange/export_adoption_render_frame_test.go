package interchange

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

func TestSchedulerRenderFrameRejectsMissingAuthorityAndBrokenLineage(t *testing.T) {
	catalog := SQLSchedulerExportInputCatalog{Bucket: "photos"}
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref}
	frame := v2draft.RenderFrameAuthority{Root: ref, Descriptor: ref,
		SelectedVersion: uuid.New(), ModelLockSHA: ref.SHA256,
		Learning: v2draft.FrozenRenderLearning{Descriptor: ref}, Ordinal: 0}
	node := v2draft.LayerRef{Contract: "WorkingImage", Ref: ref, ParentRef: ref,
		ExpectedBinding: &binding, RenderFrame: &frame}
	if err := catalog.QualifyExportRenderFrame(context.Background(), node, []byte("{}")); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("uninstalled authority: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := catalog.QualifyExportRenderFrame(ctx, node, []byte("{}")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled authority: %v", err)
	}
	node.ParentRef = v2input.Ref{ArtifactID: uuid.New(), SHA256: ref.SHA256}
	if err := catalog.QualifyExportRenderFrame(context.Background(), node, []byte("{}")); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("broken descriptor edge: %v", err)
	}
}
