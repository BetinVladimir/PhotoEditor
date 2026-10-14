package v2draft

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestDonorConfirmationCatalogNodePins(t *testing.T) {
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, CanvasHash: strings.Repeat("b", 64)}
	original := LayerRef{Contract: "DonorConfirmation", Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}, ParentRef: binding.Source, CanvasHash: binding.CanvasHash, ExpectedBinding: &binding}
	if !validDonorConfirmationCatalogNode(original, binding.AssetVersionID) {
		t.Fatal("valid node denied")
	}
	for _, mode := range []string{"no_binding", "version", "parent", "canvas", "ref", "render_frame"} {
		node := original
		switch mode {
		case "no_binding":
			node.ExpectedBinding = nil
		case "version":
			b := binding
			b.AssetVersionID = uuid.New()
			node.ExpectedBinding = &b
		case "parent":
			node.ParentRef.ArtifactID = uuid.New()
		case "canvas":
			node.CanvasHash = strings.Repeat("d", 64)
		case "ref":
			node.Ref.SHA256 = "invalid"
		case "render_frame":
			node.RenderFrame = &RenderFrameAuthority{}
		}
		if validDonorConfirmationCatalogNode(node, binding.AssetVersionID) {
			t.Fatal("invalid scope admitted", mode)
		}
	}
}
