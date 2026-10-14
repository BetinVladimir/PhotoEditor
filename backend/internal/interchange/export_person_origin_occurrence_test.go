package interchange

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type personOriginGraph map[v2input.Entry]ExportClosureNode

func (g personOriginGraph) ResolveExportNode(_ context.Context, entry v2input.Entry) (ExportClosureNode, error) {
	value, ok := g[entry]
	if !ok {
		return ExportClosureNode{}, v2input.ErrInvalid
	}
	return value, nil
}

func TestExportPersonOriginOccurrenceRequiresSelectedExactPersonBinding(t *testing.T) {
	for _, mode := range []string{"valid", "foreign_entry", "missing_parent_edge", "missing_pixel_edge", "different_mask", "unconfirmed"} {
		t.Run(mode, func(t *testing.T) {
			_, _, sourceRaw := importWorkingValidatorFixture(t)
			image, err := review.ParseWorkingImage(sourceRaw)
			if err != nil {
				t.Fatal(err)
			}
			canvas, err := canonical.Hash(image.Canvas)
			if err != nil {
				t.Fatal(err)
			}
			sourceEntry := v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(sourceRaw)}, Contract: "WorkingImage"}
			binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: sourceEntry.Ref, CanvasHash: canvas}
			model := rawSHA([]byte("model"))
			mask := review.MaskRef{Artifact: image.Pixels, Canvas: image.Canvas, TransformChain: image.TransformChain, Selector: "person", Confidence: 1, ModelLockID: &model}
			rootBody, err := canonical.Marshal(mask)
			if err != nil {
				t.Fatal(err)
			}
			root := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(rootBody)}
			pixels := v2input.Ref{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}
			confirmed := true
			if mode == "unconfirmed" {
				confirmed = false
			}
			overrideRaw, err := canonical.Marshal(map[string]any{"schema_version": 1, "input_sha256": image.Canvas.SourceSHA256, "modules": map[string]any{"strength": .7}, "revision": 1,
				"mask_refs": []any{map[string]any{"binding_id": "person-1", "purpose": "person_subject", "mask": json.RawMessage(rootBody), "confirmed": confirmed, "strength": 1}}})
			if err != nil {
				t.Fatal(err)
			}
			overrideEntry := v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(overrideRaw)}, Contract: "Override"}
			style := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("style"))}
			authored, err := canonical.Marshal(map[string]any{"schema_version": 3, "source": binding, "base_style": style, "session_corrections": nil, "learning_version_id": nil,
				"overrides": []v2input.Ref{overrideEntry.Ref}, "layer_stack": nil, "renderer_version": "v2.2", "parameter_delta": v2draft.CorrectionVector{}})
			if err != nil {
				t.Fatal(err)
			}
			frozen, err := v2draft.BuildFrozenRenderRecipe(authored, v2draft.ResolvedRecipeInputs{Binding: binding, BaseStyle: style,
				Style: v2draft.StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1}, Overrides: []v2draft.ResolvedCorrection{{Ref: overrideEntry.Ref}},
				ICCLockSHA: rawSHA([]byte("icc")), ModelLockSHA: model}, nil)
			if err != nil {
				t.Fatal(err)
			}
			parent := v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(frozen)}, Contract: "FrozenRenderRecipe"}
			entryID := uuid.New()
			recipe := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(authored)}
			plan, err := canonical.Marshal(importPlanDocument{Schema: 1, Direction: "export", Format: "photoeditor_bundle", Entries: []importPlanEntry{{ID: entryID, Binding: &binding, Recipe: &recipe}}})
			if err != nil {
				t.Fatal(err)
			}
			graph := personOriginGraph{
				sourceEntry:   {Entry: sourceEntry, Body: sourceRaw},
				parent:        {Entry: parent, Body: frozen, Children: []v2input.Entry{overrideEntry}},
				overrideEntry: {Entry: overrideEntry, Body: overrideRaw, Children: []v2input.Entry{{Ref: pixels, Contract: "MaskPixels"}}},
			}
			authority := &ExportPersonOriginOccurrence{planBody: plan, roots: []v2input.Entry{parent}, reader: graph}
			scope := PortablePersonOriginExpectation{EntryID: entryID, Source: binding, Recipe: recipe, Override: overrideEntry.Ref, MaskRef: root, MaskPixels: pixels, ModelLockSHA: model}
			switch mode {
			case "foreign_entry":
				scope.EntryID = uuid.New()
			case "missing_parent_edge":
				node := graph[parent]
				node.Children = nil
				graph[parent] = node
			case "missing_pixel_edge":
				node := graph[overrideEntry]
				node.Children = nil
				graph[overrideEntry] = node
			case "different_mask":
				rootBody = []byte(`{}`)
			}
			err = authority.QualifyPortablePersonOriginOccurrence(context.Background(), scope, rootBody)
			if (err == nil) != (mode == "valid") {
				t.Fatalf("unexpected occurrence result: %v", err)
			}
		})
	}
}
