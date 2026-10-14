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
type multiQualityParams struct {
	Target           review.ArtifactRef `json:"target"`
	TargetKind       string             `json:"target_kind"`
	Stage            string             `json:"stage"`
	ThresholdProfile string             `json:"threshold_profile_id"`
	Baseline         any                `json:"baseline"`
	Protected        []any              `json:"protected"`
	Seams            []any              `json:"seams"`
}
type MultiQualityPlan struct {
	pixels          int64
	source          v2render.MultiPreviewSource
	run, task       uuid.UUID
	params, profile json.RawMessage
	inputs          []review.ArtifactRef
}

func (p MultiQualityPlan) Run() uuid.UUID                      { return p.run }
func (p MultiQualityPlan) Source() v2render.MultiPreviewSource { return p.source }
func (p MultiQualityPlan) Params() json.RawMessage             { return bytes.Clone(p.params) }
func (p MultiQualityPlan) Inputs() []review.ArtifactRef {
	return append([]review.ArtifactRef(nil), p.inputs...)
}
func (p MultiQualityPlan) Profile() json.RawMessage { return bytes.Clone(p.profile) }

func CompileMultiQualityPlan(pin v2render.MultiPreviewSource, content v2render.MultiResultContent) (MultiQualityPlan, error) {
	var zero MultiQualityPlan
	run, err := v2render.MultiQualityRunID(pin)
	if err != nil {
		return zero, err
	}
	raw, imageRaw, err := content.PreviewDocuments(pin)
	if err != nil {
		return zero, err
	}
	result, err := v2draft.ParseImageResult(raw, pin.Binding.Result)
	if err != nil {
		return zero, err
	}
	if renderDerivativeBytesSHA(imageRaw) != result.WorkingImage.SHA256 {
		return zero, v2input.ErrInvalid
	}
	image, err := review.ParseWorkingImage(imageRaw)
	if err != nil {
		return zero, err
	}
	if image.Alpha == nil || image.DType != "float32" || image.Canvas.Width < 1 || image.Canvas.Height < 1 || image.Canvas.Width > 200000000/image.Canvas.Height {
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
	params, err := canonical.Marshal(multiQualityParams{Target: inputs[0], TargetKind: "working", Stage: "master", ThresholdProfile: "master_v1", Protected: []any{}, Seams: []any{}})
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

	return MultiQualityPlan{source: pin, pixels: pixels, run: run, task: uuid.NewSHA1(run, []byte("quality.check:master")), params: params, profile: profile, inputs: inputs}, nil
}
func PrepareMultiQualityTask(plan MultiQualityPlan, params review.ArtifactRef) (PreparedTask, error) {
	expectedRun, identityErr := v2render.MultiQualityRunID(plan.source)
	if identityErr != nil || plan.run != expectedRun || plan.pixels < 1 || plan.pixels > 200000000 {
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
	if plan.run == uuid.Nil || plan.task != uuid.NewSHA1(plan.run, []byte("quality.check:master")) || params.ID == uuid.Nil || params.SHA256 != renderDerivativeBytesSHA(plan.params) || len(plan.inputs) != 3 {
		return PreparedTask{}, v2input.ErrInvalid
	}
	seen := map[uuid.UUID]bool{params.ID: true}
	for _, ref := range plan.inputs {
		if ref.ID == uuid.Nil || seen[ref.ID] || domainDigestInvalid(ref.SHA256) {
			return PreparedTask{}, v2input.ErrInvalid
		}
		seen[ref.ID] = true
	}
	var body multiQualityParams
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
