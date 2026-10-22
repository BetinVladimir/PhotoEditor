package provenance

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	workflow "photoeditor/backend/internal/encodecontract"
)

type signingResizeParams struct {
	Image v2input.Ref `json:"image"`
	workflow.ExportSize
}
type ResizeSigningTaskResolver struct {
	Documents SigningTaskDocumentReader
	Producers SigningImageProducer
}

func (s ResizeSigningTaskResolver) ResolveSigningTaskLineage(ctx context.Context, who auth.Identity, ready ReadySigningExport, e CanonicalSigningTask) (ResolvedSigningTask, error) {
	var zero ResolvedSigningTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Documents == nil || s.Producers == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || ready.Tenant == uuid.Nil || ready.Project == uuid.Nil || ready.ExportID == uuid.Nil || e.Operation != "derivative.resize" || e.Version != "1" || e.ReviewRequired || normalizeCanonicalSigningTask(&e) != nil {
		return zero, ErrInvalid
	}
	documents := map[v2input.Ref][]byte{}
	read := func(r v2input.Ref) ([]byte, error) {
		raw, err := s.Documents.ReadSigningTaskDocument(ctx, who, ready, e, r, 1<<20)
		if err != nil {
			return nil, err
		}
		raw = append([]byte(nil), raw...)
		if !pinned(raw, r, 1<<20) {
			return nil, ErrInvalid
		}
		documents[r] = raw
		return raw, nil
	}
	paramsRaw, err := read(e.Params)
	if err != nil {
		return zero, err
	}
	p, err := decodeSigningResizeParams(paramsRaw)
	if err != nil {
		return zero, err
	}
	sourceRaw, err := read(p.Image)
	if err != nil {
		return zero, err
	}
	source, err := review.ParseWorkingImage(sourceRaw)
	if err != nil || source.DType != "float32" || source.Color.ProfileID != "work_rec2020_gamma24_v1" {
		return zero, ErrInvalid
	}
	inputs, err := canonicalTaskOutputRefs(e.Inputs)
	if err != nil {
		return zero, err
	}
	required := []v2input.Ref{p.Image, {ArtifactID: source.Pixels.ID, SHA256: source.Pixels.SHA256}}
	if source.Alpha != nil {
		required = append(required, v2input.Ref{ArtifactID: source.Alpha.ID, SHA256: source.Alpha.SHA256})
	}
	if !slices.Equal(inputs, required) {
		return zero, ErrInvalid
	}
	outputs, err := canonicalTaskOutputRefs(e.Outputs)
	if err != nil || len(outputs) != len(required)+1 {
		return zero, ErrInvalid
	}
	expected, err := expectedSigningResizeImage(source, p, outputs)
	if err != nil {
		return zero, err
	}
	resultRaw, err := read(outputs[len(outputs)-2])
	if err != nil {
		return zero, err
	}
	if _, err := review.ParseWorkingImage(resultRaw); err != nil {
		return zero, ErrInvalid
	}
	expectedRaw, err := canonical.Marshal(expected)
	if err != nil || !bytes.Equal(expectedRaw, resultRaw) {
		return zero, ErrInvalid
	}
	reportRaw, err := read(outputs[len(outputs)-1])
	if err != nil {
		return zero, err
	}
	var report signingDerivativeReport
	if decode(reportRaw, &report, 1<<20) != nil {
		return zero, ErrInvalid
	}
	same := expected.Canvas.Width == source.Canvas.Width && expected.Canvas.Height == source.Canvas.Height
	warnings := []string{}
	if same {
		warnings = []string{"no_resize_required"}
	}
	if report.Image != p.Image || report.Operation != "resize" || report.Applied == same || report.Width != expected.Canvas.Width || report.Height != expected.Canvas.Height || report.MaxDelta != 0 || report.Warnings == nil || !slices.Equal(report.Warnings, warnings) {
		return zero, ErrInvalid
	}
	producer, err := s.Producers.ResolveSigningImageProducer(ctx, who, ready, p.Image)
	if err != nil {
		return zero, err
	}
	if producer == uuid.Nil || producer == e.Task {
		return zero, ErrInvalid
	}
	for r, raw := range documents {
		current, err := s.Documents.ReadSigningTaskDocument(ctx, who, ready, e, r, 1<<20)
		if err != nil {
			return zero, err
		}
		if !bytes.Equal(current, raw) {
			return zero, ErrInvalid
		}
	}
	current, err := s.Producers.ResolveSigningImageProducer(ctx, who, ready, p.Image)
	if err != nil {
		return zero, err
	}
	if current != producer {
		return zero, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return ResolvedSigningTask{Node: SigningExecutionNode{Task: e.Task, Run: e.Run, CanonicalResult: e.Result, Attempt: e.Attempt, Fence: e.Fence, Parents: []uuid.UUID{producer}, Inputs: []v2input.Ref{p.Image}, Outputs: outputs, Action: SigningAction{Operation: e.Operation, Version: e.Version, ParamsSHA: e.Params.SHA256}}}, nil
}

func decodeSigningResizeParams(raw []byte) (signingResizeParams, error) {
	var p signingResizeParams
	if len(raw) < 2 || len(raw) > 1<<20 {
		return p, ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) {
		return p, ErrInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return p, ErrInvalid
	}
	// Python permits omitted or explicit-null dimension selectors and defaults
	// fit/allow_upscale. Normalize those only; ParamsSHA remains original bytes.
	for _, key := range []string{"long_edge", "width", "height"} {
		if bytes.Equal(fields[key], []byte("null")) {
			delete(fields, key)
		}
	}
	if _, ok := fields["fit"]; !ok {
		fields["fit"] = json.RawMessage(`"contain"`)
	}
	if _, ok := fields["allow_upscale"]; !ok {
		fields["allow_upscale"] = json.RawMessage(`false`)
	}
	normalized, err = canonical.Marshal(fields)
	if err != nil || decode(normalized, &p, 1<<20) != nil || !ref(p.Image) || p.Fit != "contain" {
		return signingResizeParams{}, ErrInvalid
	}
	long, box := p.LongEdge != nil, p.Width != nil || p.Height != nil
	if long == box || (p.Width == nil) != (p.Height == nil) {
		return signingResizeParams{}, ErrInvalid
	}
	for _, value := range []*int64{p.LongEdge, p.Width, p.Height} {
		if value != nil && (*value < 1 || *value > 100000) {
			return signingResizeParams{}, ErrInvalid
		}
	}
	return p, nil
}

func expectedSigningResizeImage(source review.WorkingImage, p signingResizeParams, outputs []v2input.Ref) (review.WorkingImage, error) {
	var zero review.WorkingImage
	if len(outputs) < 3 {
		return zero, ErrInvalid
	}
	width, height := signingEncodeDimensions(source.Canvas.Width, source.Canvas.Height, &p.ExportSize)
	if width < 1 || height < 1 || width > 200000000 || height > 200000000/width {
		return zero, ErrInvalid
	}
	space, err := canonical.Hash(map[string]any{"image": p.Image, "width": width, "height": height})
	if err != nil {
		return zero, err
	}
	target := review.Canvas{Width: width, Height: height, Orientation: 1, SpaceID: "resize:" + space[:32], SourceSHA256: source.Canvas.SourceSHA256}
	var chain struct {
		Steps []json.RawMessage `json:"steps"`
		Hash  string            `json:"hash"`
	}
	if json.Unmarshal(source.TransformChain, &chain) != nil || chain.Steps == nil || len(chain.Steps) >= 128 {
		return zero, ErrInvalid
	}
	step, err := canonical.Marshal(map[string]any{"kind": "resize", "from_canvas": source.Canvas, "to_canvas": target, "invertibility": "invertible", "valid_region": map[string]int64{"x": 0, "y": 0, "width": width, "height": height}, "pixel_centers": "half_pixel"})
	if err != nil {
		return zero, err
	}
	chain.Steps = append(chain.Steps, json.RawMessage(step))
	chain.Hash, err = canonical.Hash(map[string]any{"steps": chain.Steps})
	if err != nil {
		return zero, err
	}
	transformed, err := canonical.Marshal(chain)
	if err != nil {
		return zero, err
	}
	result := source
	result.Canvas = target
	result.TransformChain = transformed
	result.Pixels = review.ArtifactRef{ID: outputs[0].ArtifactID, SHA256: outputs[0].SHA256}
	if source.Alpha != nil {
		result.Alpha = &review.ArtifactRef{ID: outputs[1].ArtifactID, SHA256: outputs[1].SHA256}
	}
	return result, nil
}

var _ SigningTaskLineageResolver = ResizeSigningTaskResolver{}
