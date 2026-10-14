package workflow

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
)

// The base remains provenance. A changed canvas cannot be used as a same-pixel
// baseline, nor may source masks be relabeled with the output geometry.
type perspectiveQualityParams struct {
	Target           review.ArtifactRef `json:"target"`
	TargetKind       string             `json:"target_kind"`
	Stage            string             `json:"stage"`
	ThresholdProfile string             `json:"threshold_profile_id"`
	Baseline         any                `json:"baseline"`
	Protected        []any              `json:"protected"`
	Seams            []any              `json:"seams"`
}
type PerspectiveQualityPlan struct {
	pixels          int64
	source          v2render.PerspectivePreviewSource
	run, task       uuid.UUID
	params, profile json.RawMessage
	inputs          []review.ArtifactRef
}

func (p PerspectiveQualityPlan) Run() uuid.UUID                            { return p.run }
func (p PerspectiveQualityPlan) Source() v2render.PerspectivePreviewSource { return p.source }
func (p PerspectiveQualityPlan) Params() json.RawMessage                   { return bytes.Clone(p.params) }
func (p PerspectiveQualityPlan) Inputs() []review.ArtifactRef {
	return append([]review.ArtifactRef(nil), p.inputs...)
}
func (p PerspectiveQualityPlan) Profile() json.RawMessage { return bytes.Clone(p.profile) }

func CompilePerspectiveQualityPlan(bundle v2render.PerspectivePreviewBundle) (PerspectiveQualityPlan, error) {
	var zero PerspectiveQualityPlan
	pin := bundle.Source()
	if pin.Tenant == uuid.Nil || pin.Project == uuid.Nil || pin.Run == uuid.Nil || pin.Producer == uuid.Nil || pin.ResultID == uuid.Nil || pin.Attempt < 1 || pin.Fence < 1 {
		return zero, v2input.ErrInvalid
	}
	if err := v2render.ValidatePerspectivePreviewSource(pin); err != nil {
		return zero, v2input.ErrInvalid
	}
	docs := bundle.Documents()
	raw := docs[pin.Result.ArtifactID.String()]
	result, err := v2draft.ParseImageResult(raw, pin.Result)
	if err != nil {
		return zero, err
	}
	imageRaw := docs[result.WorkingImage.ArtifactID.String()]
	if renderDerivativeBytesSHA(imageRaw) != result.WorkingImage.SHA256 {
		return zero, v2input.ErrInvalid
	}
	image, err := review.ParseWorkingImage(imageRaw)
	if err != nil {
		return zero, err
	}
	if image.DType != "float32" || image.Canvas.Width < 1 || image.Canvas.Height < 1 || image.Canvas.Width > 200000000/image.Canvas.Height {
		return zero, v2input.ErrInvalid
	}
	pixels := int64(image.Canvas.Width) * int64(image.Canvas.Height)
	inputs := []review.ArtifactRef{{ID: result.WorkingImage.ArtifactID, SHA256: result.WorkingImage.SHA256}, image.Pixels}
	perPixel := int64(12)
	if image.Alpha != nil {
		inputs = append(inputs, *image.Alpha)
		perPixel += 4
	}
	seen := map[uuid.UUID]bool{}
	for _, ref := range inputs {
		if ref.ID == uuid.Nil || seen[ref.ID] || domainDigestInvalid(ref.SHA256) {
			return zero, v2input.ErrInvalid
		}
		seen[ref.ID] = true
	}
	params, err := canonical.Marshal(perspectiveQualityParams{Target: inputs[0], TargetKind: "working", Stage: "master", ThresholdProfile: "master_v1", Protected: []any{}, Seams: []any{}})
	if err != nil {
		return zero, err
	}
	profile, err := canonical.Marshal(map[string]any{"backend": "cpu", "precision": "float32", "resource_estimate": ResourceEstimate{RAMBytes: pixels*perPixel + 67108864, VRAMBytes: 0, ScratchBytes: pixels*perPixel + 1048576, OutputBytes: 1048576, PixelCount: pixels}})
	if err != nil {
		return zero, err
	}
	if _, err = ParseResourceEstimate(profile); err != nil {
		return zero, err
	}
	run := uuid.NewSHA1(pin.Run, []byte("V29.PerspectiveQuality:"+pin.Producer.String()+":"+pin.ResultID.String()+":"+pin.Result.SHA256))
	return PerspectiveQualityPlan{source: pin, pixels: pixels, run: run, task: uuid.NewSHA1(run, []byte("quality.check:master")), params: params, profile: profile, inputs: inputs}, nil
}
func PreparePerspectiveQualityTask(plan PerspectiveQualityPlan, params review.ArtifactRef) (PreparedTask, error) {
	if err := v2render.ValidatePerspectivePreviewSource(plan.source); err != nil {
		return PreparedTask{}, err
	}
	expectedRun := uuid.NewSHA1(plan.source.Run, []byte("V29.PerspectiveQuality:"+plan.source.Producer.String()+":"+plan.source.ResultID.String()+":"+plan.source.Result.SHA256))
	if plan.run != expectedRun || plan.source.Tenant == uuid.Nil || plan.source.Producer == uuid.Nil || plan.source.ResultID == uuid.Nil || plan.pixels < 1 || plan.pixels > 200000000 {
		return PreparedTask{}, v2input.ErrInvalid
	}
	perPixel := int64(12)
	if len(plan.inputs) == 3 {
		perPixel += 4
	}
	expectedProfile, err := canonical.Marshal(map[string]any{"backend": "cpu", "precision": "float32", "resource_estimate": ResourceEstimate{RAMBytes: plan.pixels*perPixel + 67108864, VRAMBytes: 0, ScratchBytes: plan.pixels*perPixel + 1048576, OutputBytes: 1048576, PixelCount: plan.pixels}})
	if err != nil || !bytes.Equal(expectedProfile, plan.profile) {
		return PreparedTask{}, v2input.ErrInvalid
	}
	if plan.run == uuid.Nil || plan.task != uuid.NewSHA1(plan.run, []byte("quality.check:master")) || params.ID == uuid.Nil || params.SHA256 != renderDerivativeBytesSHA(plan.params) || len(plan.inputs) < 2 || len(plan.inputs) > 3 {
		return PreparedTask{}, v2input.ErrInvalid
	}
	seen := map[uuid.UUID]bool{params.ID: true}
	for _, ref := range plan.inputs {
		if ref.ID == uuid.Nil || seen[ref.ID] || domainDigestInvalid(ref.SHA256) {
			return PreparedTask{}, v2input.ErrInvalid
		}
		seen[ref.ID] = true
	}
	var body perspectiveQualityParams
	if json.Unmarshal(plan.params, &body) != nil || body.Target != plan.inputs[0] || body.TargetKind != "working" || body.Stage != "master" || body.ThresholdProfile != "master_v1" || body.Baseline != nil || len(body.Protected) != 0 || len(body.Seams) != 0 {
		return PreparedTask{}, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(body)
	if err != nil || !bytes.Equal(exact, plan.params) {
		return PreparedTask{}, v2input.ErrInvalid
	}
	if _, err = ParseResourceEstimate(plan.profile); err != nil {
		return PreparedTask{}, err
	}
	digest, err := repairInputDigest("quality.check", "1", params, plan.inputs)
	if err != nil {
		return PreparedTask{}, err
	}
	return PreparedTask{ID: plan.task, NodeKey: "quality.check:master", Operation: "quality.check", Version: "1", ResourceClass: "cpu", TimeoutSeconds: 600, Params: params, Inputs: plan.Inputs(), InputDigest: digest, ExecutionProfile: plan.Profile()}, nil
}
