package provenance

import (
	"context"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/workflow"
	"testing"
)

func TestPresentationClaimsNeverOmitUnsupportedActions(t *testing.T) {
	for _, operation := range []string{"image.inpaint", "render", "metadata.write"} {
		version := "1"
		if operation == "metadata.write" {
			version = "2"
		}
		result, err := (PresentationSigningTaskResolver{}).ResolveSigningTaskLineage(context.Background(), auth.Identity{}, ReadySigningExport{}, CanonicalSigningTask{Operation: operation, Version: version})
		if err == nil || result.Node.Action.Operation != "" {
			t.Fatal("unsupported action silently represented")
		}
	}
}

func TestSigningEncodeDimensionsMatchValidatedPresentationSizing(t *testing.T) {
	edge, large, width, height := int64(200), int64(1000), int64(100), int64(200)
	for _, test := range []struct {
		size *workflow.ExportSize
		w, h int64
	}{
		{nil, 400, 300},
		{&workflow.ExportSize{LongEdge: &edge, Fit: "contain"}, 200, 150},
		{&workflow.ExportSize{LongEdge: &large, Fit: "contain"}, 400, 300},
		{&workflow.ExportSize{LongEdge: &large, Fit: "contain", AllowUpscale: true}, 1000, 750},
		{&workflow.ExportSize{Width: &width, Height: &height, Fit: "contain"}, 100, 75},
	} {
		w, h := signingEncodeDimensions(400, 300, test.size)
		if w != test.w || h != test.h {
			t.Fatal("wrong presentation dimensions", w, h)
		}
	}
}
