package v2region

import (
	"context"
	"encoding/json"

	"photoeditor/backend/internal/canonical"
)

// RegionQualifiedCost is installation-owned evidence for the entire effective
// recipe. Operator peaks exclude source mapping, the retained full RGBA canvas
// and presentation/staging costs added by the compiler. Costs must include all
// enabled operators, model loading and their simultaneously live intermediates.
// A syntactically valid record alone is not evidence of qualification.
type RegionQualifiedCost struct {
	RuntimeSHA, ModelLockSHA, ICCLockSHA, PolicySHA string
	RAMBytes, ScratchBytes, VRAMBytes, OutputBytes  int64
	Pin                                             RegionCalibrationPin
}

// RegionCostQualifier must recheck installed qualification and resolve the
// complete effective StyleV2, including nested/adaptive modules, for this plan.
// It must return unavailable when any required operator or cost is unqualified.
// Public requests cannot supply this dependency or its result.
type RegionCostQualifier interface {
	QualifyRegionCost(context.Context, FrozenRegionPlan) (RegionQualifiedCost, error)
}

// CompiledRegionProfiles is a QueueService profile provider. The qualifier is
// mandatory; this compiler does not silently qualify a runtime or enable SQL
// operation availability. Admission still rechecks policy authority and quota.
type CompiledRegionProfiles struct{ Qualifier RegionCostQualifier }

func (p CompiledRegionProfiles) CompileRegionProfile(ctx context.Context, plan FrozenRegionPlan) (RegionExecutionPolicy, error) {
	if err := ctx.Err(); err != nil {
		return RegionExecutionPolicy{}, err
	}
	if p.Qualifier == nil {
		return RegionExecutionPolicy{}, ErrRegionUnavailable
	}
	if _, err := plan.SourceShape(); err != nil {
		return RegionExecutionPolicy{}, err
	}
	// Detach slices before crossing an installation dependency boundary.
	owned := plan
	owned.body = append([]byte(nil), plan.body...)
	owned.inputs = append(owned.inputs[:0:0], plan.inputs...)
	cost, err := p.Qualifier.QualifyRegionCost(ctx, owned)
	if err != nil {
		return RegionExecutionPolicy{}, err
	}
	if err := ctx.Err(); err != nil {
		return RegionExecutionPolicy{}, err
	}
	return CompileRegionExecutionPolicy(plan, cost)
}

type RegionResourceEstimate struct {
	PixelCount   int64 `json:"pixel_count"`
	RAMBytes     int64 `json:"ram_bytes"`
	ScratchBytes int64 `json:"scratch_bytes"`
	VRAMBytes    int64 `json:"vram_bytes"`
	OutputBytes  int64 `json:"output_bytes"`
}

// CompileRegionExecutionPolicy adds conservative orchestration costs to already
// qualified operator peaks. It uses verified full-canvas evidence, never ROI
// dimensions for the renderer. Legacy alpha normalization is always reserved.
func CompileRegionExecutionPolicy(plan FrozenRegionPlan, cost RegionQualifiedCost) (RegionExecutionPolicy, error) {
	invalid := func() (RegionExecutionPolicy, error) { return RegionExecutionPolicy{}, ErrRegionInvalid }
	shape, err := plan.SourceShape()
	if err != nil {
		return invalid()
	}
	document, err := plan.decode()
	if err != nil || !validRegionSHA(cost.RuntimeSHA) || !validRegionSHA(cost.PolicySHA) ||
		!validRegionSHA(cost.ModelLockSHA) || !validRegionSHA(cost.ICCLockSHA) ||
		cost.ModelLockSHA != document.FrozenRecipe.ModelLockSHA || cost.ICCLockSHA != document.FrozenRecipe.ICCLockSHA {
		return invalid()
	}
	if err := cost.Pin.validate(plan, cost); err != nil {
		return invalid()
	}
	const safe int64 = 9007199254740991
	// CUDA qualification must provide a measured positive device peak; no CPU or
	// unknown-device fallback is allowed for Region task admission.
	if cost.VRAMBytes < 1 {
		return invalid()
	}
	for _, n := range []int64{cost.RAMBytes, cost.ScratchBytes, cost.VRAMBytes, cost.OutputBytes} {
		if n < 0 || n > safe {
			return invalid()
		}
	}
	source, err := shape.ResourceBudget()
	if err != nil {
		return invalid()
	}
	roi := document.Geometry.OutputWidth * document.Geometry.OutputHeight
	if roi < 1 || roi > 1024*1024 {
		return invalid()
	}
	// Source budget includes row/tile validation. Full rendered RGB+alpha stays
	// live through presentation. Reserve all ROI conversion/resize temporaries,
	// a 3GiB StyleV2 parser floor, encoded derivative and bounded JSON manifest.
	const outputCap int64 = 16*1024*1024 + 8*1024*1024
	resources := RegionResourceEstimate{
		PixelCount:   source.PixelCount,
		RAMBytes:     max(int64(3)<<30, source.RAMBytes+16*source.PixelCount+64*roi) + cost.RAMBytes,
		ScratchBytes: source.ScratchBytes + 16*source.PixelCount + 5*roi + 2*1024*1024 + outputCap + cost.ScratchBytes,
		VRAMBytes:    cost.VRAMBytes,
		OutputBytes:  outputCap + cost.OutputBytes,
	}
	for _, n := range []int64{resources.RAMBytes, resources.ScratchBytes, resources.OutputBytes} {
		if n > safe {
			return invalid()
		}
	}
	profile, err := canonical.Marshal(struct {
		Backend       string                 `json:"backend"`
		Renderer      string                 `json:"renderer_version"`
		RuntimeSHA    string                 `json:"region_runtime_sha256"`
		ModelSHA      string                 `json:"model_lock_sha"`
		ICCSHA        string                 `json:"icc_lock_sha"`
		AlphaMode     string                 `json:"alpha_mode"`
		Resources     RegionResourceEstimate `json:"resource_estimate"`
		Qualification RegionCalibrationPin   `json:"region_qualification"`
	}{"cuda", "v2.2", cost.RuntimeSHA, cost.ModelLockSHA, cost.ICCLockSHA, "normalize_uint16", resources, cost.Pin})
	if err != nil || !json.Valid(profile) {
		return invalid()
	}
	return ownRegionExecutionPolicy(RegionExecutionPolicy{Profile: profile, PolicySHA: cost.PolicySHA, ReservedBytes: resources.OutputBytes})
}
