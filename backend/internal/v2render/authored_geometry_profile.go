package v2render

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

const AuthoredGeometryKernelVersion = "v2.authored.geometry.1"

// The exact additive generated bundle pin is supplied by the schema rollout;
// it is distinct from the segment/final installation pin.
const AuthoredGeometrySchemasSHA = "338b60d6fd3e232eb78ceb71d559a8eb4a9e82170d1088d4484a09907d19984b"

type AuthoredGeometryCalibrationCatalog interface {
	ResolveAuthoredGeometry(context.Context, RenderQualification) (RenderCalibration, error)
}
type AuthoredGeometryExecutionProfile struct {
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

// CompileAuthoredGeometryExecutionProfile selects a genuine operator-pinned
// calibration for this CPU blend/replay build. Caller dimensions are actual
// target-canvas geometry; this compiler grants no SQL/source authority.
func CompileAuthoredGeometryExecutionProfile(ctx context.Context, catalog AuthoredGeometryCalibrationCatalog, q RenderQualification, width, height int64, icc string) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if catalog == nil || q.Backend != "cpu" || !authoredFinalDigest(icc) || !authoredFinalDigest(AuthoredGeometrySchemasSHA) {
		return nil, v2input.ErrInvalid
	}
	calibration, err := catalog.ResolveAuthoredGeometry(ctx, q)
	if err != nil {
		return nil, err
	}
	if err = calibration.validate(q); err != nil {
		return nil, err
	}
	selected, err := calibration.Select(q, width, height, icc)
	if err != nil {
		return nil, err
	}
	if selected.Provider.VRAMBytes != 0 {
		return nil, v2input.ErrInvalid
	}
	// Calibrated provider peak includes actual native replay/parallel registration.
	// Deterministic owner accounting remains additive, never measured evidence.
	if width < 1 || height < 1 || width > 200000000/height {
		return nil, v2input.ErrInvalid
	}
	pixels := width * height
	peak := selected.Provider
	const safe = 9007199254740991
	if peak.RAMBytes > safe-96*pixels || peak.ScratchBytes > safe-48*pixels-65536 || peak.OutputBytes > safe-40*pixels-67108864 {
		return nil, v2input.ErrInvalid
	}
	peak.RAMBytes += 96 * pixels
	peak.ScratchBytes += 48*pixels + 65536
	peak.OutputBytes += 40*pixels + 67108864
	budget, err := RenderResources(width, height, 2, 2, calibration.TileSize, calibration.AlphaMode, peak)
	if err != nil {
		return nil, err
	}
	calibrationSHA, err := canonical.Hash(calibration)
	if err != nil {
		return nil, err
	}
	profile := AuthoredGeometryExecutionProfile{Backend: "cpu", Renderer: "v2.2", KernelVersion: AuthoredGeometryKernelVersion, Operation: "render.authored_geometry", Version: "1", ContractsSHA: AuthoredGeometrySchemasSHA, RuntimeSHA: q.RuntimeSHA, ModelSHA: q.ModelSHA, ICCSHA: icc, TileSize: calibration.TileSize, AlphaMode: calibration.AlphaMode, Resources: budget, Provider: selected.Provider, Qualification: RenderQualificationPin{RenderQualification: q, CalibrationSHA: calibrationSHA}, RequiredReview: true}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return canonical.Marshal(profile)
}
