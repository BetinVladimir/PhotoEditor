package workflow

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/exportpreset"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestDeliveryInitialTaskSelectsStageAndPinsV2Draft(t *testing.T) {
	for _, kind := range []string{"encode", "resize", "sharpen", "both"} {
		t.Run(kind, func(t *testing.T) {
			plan, parent, child, raw, base := deliveryExportPlanFixture(t, func(config *exportpreset.Config) {
				if kind == "resize" || kind == "both" {
					edge := 200
					config.Size = &exportpreset.Size{LongEdge: &edge, Fit: "contain", AllowUpscale: false}
				}
				if kind == "sharpen" || kind == "both" {
					config.Sharpen = &exportpreset.Sharpen{Radius: 1, Strength: .4, Threshold: .02}
				}
			})
			registry, err := LoadRegistry()
			if err != nil {
				t.Fatal(err)
			}
			node, err := CompileDeliveryInitialNode(plan, parent, child, raw, base.TargetICCSHA256, registry)
			if err != nil {
				t.Fatal(err)
			}
			operation := "derivative.resize"
			if kind == "encode" {
				operation = "export.encode"
			}
			if kind == "sharpen" {
				operation = "derivative.sharpen"
			}
			workflow, _ := v2input.WorkflowID(child.RunID)
			expected, _ := v2input.TaskID(workflow, operation, 0)
			if node.Operation != operation || node.ID != expected || node.Key != operation+":0" {
				t.Fatal("V2 identity/stage mismatch")
			}
			image, err := review.ParseWorkingImage(raw)
			if err != nil {
				t.Fatal(err)
			}
			resolved := []v2input.Entry{
				{Ref: child.Binding.Source, Contract: "WorkingImage"},
				{Ref: child.Recipe, Contract: "EditingRecipeV3"},
				{Ref: child.Preset.Config, Contract: "ExportPresetConfig"},
				{Ref: v2input.Ref{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}, Contract: "NpyArray"},
			}
			if image.Alpha != nil {
				resolved = append(resolved, v2input.Entry{Ref: v2input.Ref{ArtifactID: image.Alpha.ID, SHA256: image.Alpha.SHA256}, Contract: "NpyArray"})
			}
			params := v2input.Ref{ArtifactID: uuid.New(), SHA256: renderDerivativeBytesSHA(node.Params)}
			limits := DeliveryDerivativeLimits{RAMBytes: 4 << 30, ScratchBytes: 2 << 30, OutputBytes: 1 << 30}
			frozen, err := FreezeDeliveryInitialTask(plan, parent, child, raw, base.TargetICCSHA256, registry, params, resolved, limits)
			if err != nil {
				t.Fatal(err)
			}
			draft, err := frozen.DraftTask()
			if err != nil || draft.ID != node.ID || draft.ParamsSHA != params.SHA256 || draft.InputDigest == "" {
				t.Fatal("invalid frozen draft", err)
			}
			estimate, err := ParseResourceEstimate(draft.ExecutionProfile)
			if err != nil || estimate.VRAMBytes != 0 {
				t.Fatal("CPU profile", err)
			}
			draft.ExecutionProfile[0] = 'x'
			resolved[0] = v2input.Entry{}
			again, err := frozen.DraftTask()
			if err != nil || again.ExecutionProfile[0] != '{' {
				t.Fatal("borrowed profile", err)
			}
			if _, err := frozen.PublishInputs(context.Background(), nil); err == nil {
				t.Fatal("nil publisher accepted")
			}
			if _, err := FreezeDeliveryInitialTask(plan, parent, child, raw, base.TargetICCSHA256, registry, params, resolved, limits); err == nil {
				t.Fatal("missing source accepted")
			}
		})
	}
}
