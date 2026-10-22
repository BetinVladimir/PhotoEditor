package v2render

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

// Synthetic compiler inputs only; these are not installed qualifications.
func segmentProfileFixture(t *testing.T, active bool) (RenderDraftRequest, CompiledAuthoredSegmentParams, []byte, []byte, RenderCalibration) {
	t.Helper()
	pin := strings.Repeat("a", 64)
	ref := func(raw []byte) v2input.Ref {
		sum := sha256.Sum256(raw)
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	canvas := map[string]any{"width": 4, "height": 3, "orientation": 1, "space_id": "source", "source_sha256": pin}
	chainSHA, _ := canonical.Hash(map[string]any{"steps": []any{}})
	source, _ := canonical.Marshal(map[string]any{"pixels": v2input.Ref{ArtifactID: uuid.New(), SHA256: pin}, "alpha": nil, "dtype": "float32", "canvas": canvas, "color": map[string]any{"profile_id": "linear_srgb_v1", "encoding": "linear", "icc_sha256": pin}, "transform_chain": map[string]any{"steps": []any{}, "hash": chainSHA}})
	canvasSHA, _ := canonical.Hash(canvas)
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(source), CanvasHash: canvasSHA}
	style := v2input.Ref{ArtifactID: uuid.New(), SHA256: pin}
	opacity := 0.
	if active {
		opacity = .4
	}
	stack, _ := canonical.Marshal(map[string]any{"schema_version": 1, "source": binding, "layers": []any{map[string]any{"id": uuid.New(), "enabled": active, "opacity": opacity, "blend": "screen", "operation": "adjust.exposure", "operation_version": "1", "params": ref([]byte(`{"schema_version":1,"ev":0.2}`)), "mask": map[string]any{"op": "ref", "mask": ref([]byte("mask")), "canvas_hash": canvasSHA}}}})
	stackRef := ref(stack)
	authored, _ := canonical.Marshal(map[string]any{"schema_version": 3, "source": binding, "base_style": style, "session_corrections": nil, "learning_version_id": nil, "overrides": []v2input.Ref{}, "layer_stack": stackRef, "renderer_version": "v2.2", "parameter_delta": v2draft.CorrectionVector{EV: .1}})
	frozen, err := v2draft.BuildFrozenRenderRecipe(authored, v2draft.ResolvedRecipeInputs{Binding: binding, BaseStyle: style, Style: v2draft.StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1}, ICCLockSHA: pin, ModelLockSHA: pin}, stack)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v2draft.DecodeFrozenRenderRecipe(frozen); err != nil { t.Fatalf("fixture frozen recipe decode: %v", err) }
	request := RenderDraftRequest{Tenant: uuid.New(), Binding: binding, RecipeRef: ref(frozen), RecipeRaw: frozen, SourceRaw: source, LayerRaw: stack}
	workflow := uuid.New()
	instruction, err := v2input.TaskID(workflow, "render.authored_segment", 1)
	if err != nil {
		t.Fatal(err)
	}
	params := AuthoredSegmentParams{SchemaVersion: 1, TenantID: request.Tenant, ParentRunID: uuid.New(), WorkflowID: workflow, InstructionID: instruction, Ordinal: 1, ProgramSHA256: pin, PredecessorSHA256: pin, Recipe: request.RecipeRef, BaseReceipt: ref([]byte("base")), Source: binding, PhaseReceipts: []v2input.Ref{}}
	current := stack
	if active {
		params.CurrentLayers = &stackRef
	} else {
		current = nil
	}
	compiled, err := CompileAuthoredSegmentParams(params)
	if err != nil {
		t.Fatal(err)
	}
	calibration := RenderCalibration{Qualification: RenderQualification{ModelSHA: pin, RuntimeSHA: strings.Repeat("b", 64), Backend: "cpu", DeviceSHA: strings.Repeat("c", 64), DatasetSHA: strings.Repeat("d", 64)}, EvidenceSHA: strings.Repeat("e", 64), ICCSHA: pin, TileSize: 512, AlphaMode: "require_float32", Samples: []RenderCalibrationSample{{Width: 4, Height: 3, Provider: RenderProviderPeak{RAMBytes: 100, ScratchBytes: 100, OutputBytes: 100}}}}
	return request, compiled, source, current, calibration
}

func TestAuthoredSegmentProfileRealCurrentLayerBudgetAndPins(t *testing.T) {
	r, p, source, layers, c := segmentProfileFixture(t, true)
	raw, err := CompileAuthoredSegmentExecutionProfile(r, p, source, layers, c.Qualification, c)
	if err != nil {
		t.Fatal(err)
	}
	var got AuthoredSegmentExecutionProfile
	if json.Unmarshal(raw, &got) != nil {
		t.Fatal("invalid profile")
	}
	selected, err := c.Select(c.Qualification, 4, 3, c.ICCSHA)
	if err != nil {
		t.Fatal(err)
	}
	peak := selected.Provider
	peak.RAMBytes += 40 * 12
	peak.ScratchBytes += 4*12 + 8192
	want, err := RenderResources(4, 3, 1, 1, c.TileSize, c.AlphaMode, peak)
	if err != nil {
		t.Fatal(err)
	}
	if got.Resources != want || got.Backend != "cpu" || got.KernelVersion != AuthoredSegmentKernelVersion || got.ContractsSHA != AuthoredSegmentSchemasSHA || got.Operation != "render.authored_segment" || got.Version != "1" || !got.RequiredReview || got.Provider != selected.Provider {
		t.Fatal("segment profile differs from actual retained/mask budget")
	}
}
func TestAuthoredSegmentProfileDisabledOccurrenceExplicitNull(t *testing.T) {
	r, p, source, layers, c := segmentProfileFixture(t, false)
	raw, err := CompileAuthoredSegmentExecutionProfile(r, p, source, layers, c.Qualification, c)
	if err != nil {
		t.Fatal(err)
	}
	var profile AuthoredSegmentExecutionProfile
	if json.Unmarshal(raw, &profile) != nil {
		t.Fatal("invalid disabled profile")
	}
	selected, err := c.Select(c.Qualification, 4, 3, c.ICCSHA)
	if err != nil {
		t.Fatal(err)
	}
	peak := selected.Provider
	peak.RAMBytes += 40 * 12
	peak.ScratchBytes += 4*12 + 8192
	final, err := RenderResources(4, 3, 1, 1, c.TileSize, c.AlphaMode, peak)
	if err != nil {
		t.Fatal(err)
	}
	// Frozen vector produced by actual Python render_resources/admit_render.
	var workerFinal LayerResourceEstimate
	if json.Unmarshal([]byte(`{"pixel_count":12,"ram_bytes":268579764,"scratch_bytes":67257880,"output_bytes":33554840,"vram_bytes":0}`), &workerFinal) != nil || profile.Resources != workerFinal {
		t.Fatal("profile differs from actual worker zero-mask final vector")
	}
	if profile.Resources.RAMBytes < final.RAMBytes || profile.Resources.ScratchBytes < final.ScratchBytes || profile.Resources.OutputBytes < final.OutputBytes {
		t.Fatal("zero-mask segment underbudgets final change-map admission")
	}
	p.Params.CurrentLayers = &p.Params.Recipe
	p, _ = CompileAuthoredSegmentParams(p.Params)
	if _, err := CompileAuthoredSegmentExecutionProfile(r, p, source, r.LayerRaw, c.Qualification, c); err == nil {
		t.Fatal("disabled occurrence gained active layer document")
	}
}
func TestAuthoredSegmentProfileRejectsChangedSourceAndUncoveredCalibration(t *testing.T) {
	for _, variant := range []string{"source", "layerbytes", "instruction", "backend", "model", "icc", "dimensions", "vram", "compilerbytes"} {
		t.Run(variant, func(t *testing.T) {
			r, p, source, layers, c := segmentProfileFixture(t, true)
			switch variant {
			case "source":
				source = append(source, ' ')
			case "layerbytes":
				layers = append(layers, ' ')
			case "instruction":
				p.Params.InstructionID = uuid.New()
				p, _ = CompileAuthoredSegmentParams(p.Params)
			case "backend":
				c.Qualification.Backend = "mps"
			case "model":
				c.Qualification.ModelSHA = strings.Repeat("f", 64)
			case "icc":
				c.ICCSHA = strings.Repeat("f", 64)
			case "dimensions":
				c.Samples[0].Width = 3
			case "vram":
				c.Samples[0].Provider.VRAMBytes = 1
			case "compilerbytes":
				p.JSON = append(p.JSON, ' ')
			}
			if _, err := CompileAuthoredSegmentExecutionProfile(r, p, source, layers, c.Qualification, c); err == nil {
				t.Fatal("invalid segment profile accepted")
			}
		})
	}
}
