package v2render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// AuthoredSegmentSchemasSHA includes program2, singleton segment and protected
// style-binding contracts. Old pins remain reader-only compatibility inputs.
const AuthoredSegmentSchemasSHA = "55c5e551578614890def6135458f535fa71d1d1725a6525d490588b3f29a4772"

const AuthoredSegmentKernelVersion = "v2.authored.segment.1"

type AuthoredSegmentExecutionProfile struct {
	Backend        string                 `json:"backend"`
	Renderer       string                 `json:"renderer_version"`
	KernelVersion  string                 `json:"authored_kernel_version"`
	Operation      string                 `json:"operation"`
	Version        string                 `json:"operation_version"`
	ContractsSHA   string                 `json:"contracts_sha"`
	RuntimeSHA     string                 `json:"render_runtime_sha"`
	ModelSHA       string                 `json:"model_lock_sha"`
	ICCSHA         string                 `json:"icc_lock_sha"`
	TileSize       int                    `json:"tile_size"`
	AlphaMode      string                 `json:"alpha_mode"`
	Resources      LayerResourceEstimate  `json:"resource_estimate"`
	Provider       RenderProviderPeak     `json:"retained_stage_peak"`
	Qualification  RenderQualificationPin `json:"native_qualification_pin"`
	RequiredReview bool                   `json:"review_required"`
}

// CompileAuthoredSegmentExecutionProfile freezes one original program2 layer
// occurrence against its actual canonical predecessor and current layer bytes.
// It does not qualify caller data or grant authority: SQL must attest the exact
// instruction/current-source scope and operation-specific calibration policy.
// CPU is truthful for this compositor; task admission retains the GPU photo RAM
// capacity class separately. No upstream CUDA work is relabelled here.
func CompileAuthoredSegmentExecutionProfile(request RenderDraftRequest, params CompiledAuthoredSegmentParams, targetRaw, currentLayersRaw []byte, qualification RenderQualification, calibration RenderCalibration) (json.RawMessage, error) {
	fail := func() (json.RawMessage, error) { return nil, v2input.ErrInvalid }
	expectedInstruction, identityErr := v2input.TaskID(params.Params.WorkflowID, "render.authored_segment", params.Params.Ordinal)
	if identityErr != nil || expectedInstruction != params.Params.InstructionID {
		return fail()
	}
	verified, err := CompileAuthoredSegmentParams(params.Params)
	if err != nil || !bytes.Equal(verified.JSON, params.JSON) {
		return fail()
	}
	if qualification.Backend != "cpu" || params.Params.TenantID != request.Tenant || params.Params.Recipe != request.RecipeRef {
		return fail()
	}
	evidence, err := CompileRenderEvidence(request.Binding, request.RecipeRef, request.RecipeRaw, request.SourceRaw)
	if err != nil {
		return fail()
	}
	if qualification.ModelSHA != evidence.Recipe.Scalars.ModelLockSHA {
		return fail()
	}
	if err = calibration.validate(qualification); err != nil {
		return fail()
	}
	if len(targetRaw) < 2 || len(targetRaw) > 1048576 {
		return fail()
	}
	sum := sha256.Sum256(targetRaw)
	wire, err := canonical.Bytes(targetRaw)
	if err != nil || !bytes.Equal(wire, targetRaw) || hex.EncodeToString(sum[:]) != params.Params.Source.Source.SHA256 {
		return fail()
	}
	target, err := review.ParseWorkingImage(targetRaw)
	if err != nil {
		return fail()
	}
	canvasSHA, err := canonical.Hash(target.Canvas)
	if err != nil || canvasSHA != params.Params.Source.CanvasHash || params.Params.Source.AssetVersionID != request.Binding.AssetVersionID || target.Color != evidence.Source.Color || target.Canvas.SourceSHA256 != evidence.Source.Canvas.SourceSHA256 {
		return fail()
	}
	selected, err := calibration.Select(qualification, int64(target.Canvas.Width), int64(target.Canvas.Height), evidence.Recipe.Scalars.ICCLockSHA)
	if err != nil {
		return fail()
	}
	if selected.Provider.VRAMBytes != 0 {
		return fail()
	}
	// Matches actual segment prepare_geometry_final: retained arrays plus real change-map
	// scratch. No sampled memory observation is presented as a measured peak.
	pixels := int64(target.Canvas.Width) * int64(target.Canvas.Height)
	peak := selected.Provider
	peak.RAMBytes += 40 * pixels
	peak.ScratchBytes += 4*pixels + 8192
	// Authenticate the full original stack, then select its exact ordinal.
	var original struct {
		LayerStack *v2input.Ref `json:"layer_stack"`
	}
	if json.Unmarshal(evidence.Recipe.Recipe, &original) != nil || original.LayerStack == nil {
		return fail()
	}
	stackHash := sha256.Sum256(request.LayerRaw)
	if hex.EncodeToString(stackHash[:]) != original.LayerStack.SHA256 {
		return fail()
	}
	originalPlan, err := Compile(request.LayerRaw, request.Binding)
	if err != nil || params.Params.Ordinal > len(originalPlan.Layers) {
		return fail()
	}
	declaration := originalPlan.Layers[params.Params.Ordinal-1].Layer
	if declaration.Active() {
		switch declaration.Operation {
		case "optical.lens", "optical.perspective", "optical.panorama", "optical.focus_stack", "image.upscale":
			return fail()
		}
	}
	plan := Plan{Source: params.Params.Source}
	if params.Params.CurrentLayers == nil {
		if declaration.Active() || len(currentLayersRaw) != 0 {
			return fail()
		}
	} else {
		if !declaration.Active() || len(currentLayersRaw) < 2 || len(currentLayersRaw) > 1048576 {
			return fail()
		}
		sum := sha256.Sum256(currentLayersRaw)
		wire, parseErr := canonical.Bytes(currentLayersRaw)
		if parseErr != nil || !bytes.Equal(wire, currentLayersRaw) || hex.EncodeToString(sum[:]) != params.Params.CurrentLayers.SHA256 {
			return fail()
		}
		plan, err = Compile(currentLayersRaw, params.Params.Source)
		if err != nil || len(plan.Layers) != 1 {
			return fail()
		}
		current := plan.Layers[0].Layer
		if current.ID != declaration.ID || current.Operation != declaration.Operation || current.OperationVersion != declaration.OperationVersion || current.Enabled != declaration.Enabled || current.Opacity != declaration.Opacity || current.Blend != declaration.Blend {
			return fail()
		}
	}
	// BudgetRender counts each reachable active mask node and deduplicates refs,
	// including the current protection ref. Geometry-final retains40 bytes/pixel
	// in addition to the calibrated provider owners and writes4-byte changes.
	budget, err := plan.BudgetRender(int64(target.Canvas.Width), int64(target.Canvas.Height), params.Params.ProtectedMask, calibration.TileSize, calibration.AlphaMode, peak)
	if err != nil {
		return fail()
	}
	resources := budget.Resources
	// prepare_geometry_final always admits a one-ref/one-node change-map stage,
	// even if the real layer graph has zero refs (disabled or constant masks).
	// Keep actual mask counts; take the componentwise peak of both stages.
	finalResources, err := RenderResources(int64(target.Canvas.Width), int64(target.Canvas.Height), 1, 1, calibration.TileSize, calibration.AlphaMode, peak)
	if err != nil {
		return fail()
	}
	resources.RAMBytes = max(resources.RAMBytes, finalResources.RAMBytes)
	resources.ScratchBytes = max(resources.ScratchBytes, finalResources.ScratchBytes)
	resources.OutputBytes = max(resources.OutputBytes, finalResources.OutputBytes)
	resources.VRAMBytes = max(resources.VRAMBytes, finalResources.VRAMBytes)
	calibrationSHA, err := canonical.Hash(calibration)
	if err != nil {
		return fail()
	}
	profile := AuthoredSegmentExecutionProfile{Backend: "cpu", Renderer: "v2.2", KernelVersion: AuthoredSegmentKernelVersion, Operation: "render.authored_segment", Version: "1", ContractsSHA: AuthoredSegmentSchemasSHA,
		RuntimeSHA: qualification.RuntimeSHA, ModelSHA: qualification.ModelSHA, ICCSHA: evidence.Recipe.Scalars.ICCLockSHA,
		TileSize: calibration.TileSize, AlphaMode: calibration.AlphaMode, Resources: resources, Provider: selected.Provider,
		Qualification: RenderQualificationPin{RenderQualification: qualification, CalibrationSHA: calibrationSHA}, RequiredReview: true}
	return canonical.Marshal(profile)
}
