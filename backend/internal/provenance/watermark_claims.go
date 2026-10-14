package provenance

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"slices"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type signingWatermarkParams struct {
	Image     v2input.Ref `json:"image"`
	Watermark v2input.Ref `json:"watermark"`
	Opacity   float64     `json:"opacity"`
	Anchor    string      `json:"anchor"`
	MarginX   float64     `json:"margin_x"`
	MarginY   float64     `json:"margin_y"`
	MaxWidth  float64     `json:"max_width_fraction"`
	Algorithm string      `json:"algorithm"`
}
type WatermarkSigningTaskResolver struct {
	Documents SigningTaskDocumentReader
	Producers SigningImageProducer
}

func (s WatermarkSigningTaskResolver) ResolveSigningTaskLineage(ctx context.Context, who auth.Identity, ready ReadySigningExport, e CanonicalSigningTask) (ResolvedSigningTask, error) {
	var zero ResolvedSigningTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Documents == nil || s.Producers == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || ready.Tenant == uuid.Nil || ready.Project == uuid.Nil || ready.ExportID == uuid.Nil || e.Operation != "derivative.watermark" || e.Version != "1" || e.ReviewRequired || normalizeCanonicalSigningTask(&e) != nil {
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
	p, err := decodeSigningWatermarkParams(paramsRaw)
	if err != nil {
		return zero, err
	}
	sourceRaw, err := read(p.Image)
	if err != nil {
		return zero, err
	}
	source, err := review.ParseWorkingImage(sourceRaw)
	if err != nil {
		return zero, ErrInvalid
	}
	markRaw, err := read(p.Watermark)
	if err != nil {
		return zero, err
	}
	mark, err := review.ParseWorkingImage(markRaw)
	if err != nil {
		return zero, ErrInvalid
	}
	if source.DType != "float32" || mark.DType != "float32" || source.Color.ProfileID != "work_rec2020_gamma24_v1" || mark.Color != source.Color || mark.Alpha == nil || !signingWatermarkFits(source.Canvas, mark.Canvas, p) {
		return zero, ErrInvalid
	}
	required := func(ref v2input.Ref, image review.WorkingImage) []v2input.Ref {
		refs := []v2input.Ref{ref, {ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}}
		if image.Alpha != nil {
			refs = append(refs, v2input.Ref{ArtifactID: image.Alpha.ID, SHA256: image.Alpha.SHA256})
		}
		return refs
	}
	inputs, err := canonicalTaskInputRefs(e.Inputs)
	if err != nil || !slices.Equal(inputs, append(required(p.Image, source), required(p.Watermark, mark)...)) {
		return zero, ErrInvalid
	}
	outputs, err := canonicalTaskOutputRefs(e.Outputs)
	count := 3
	if source.Alpha != nil {
		count = 4
	}
	if err != nil || len(outputs) != count {
		return zero, ErrInvalid
	}
	resultRaw, err := read(outputs[count-2])
	if err != nil {
		return zero, err
	}
	if _, err := review.ParseWorkingImage(resultRaw); err != nil {
		return zero, ErrInvalid
	}
	expected := source
	expected.Pixels = review.ArtifactRef{ID: outputs[0].ArtifactID, SHA256: outputs[0].SHA256}
	if source.Alpha != nil {
		expected.Alpha = &review.ArtifactRef{ID: outputs[1].ArtifactID, SHA256: outputs[1].SHA256}
	}
	expectedRaw, err := canonical.Marshal(expected)
	if err != nil || !bytes.Equal(expectedRaw, resultRaw) {
		return zero, ErrInvalid
	}
	reportRaw, err := read(outputs[count-1])
	if err != nil {
		return zero, err
	}
	var report signingDerivativeReport
	if decode(reportRaw, &report, 1<<20) != nil {
		return zero, ErrInvalid
	}
	warnings := []string{}
	if p.Opacity == 0 {
		warnings = []string{"zero_opacity"}
	}
	if report.Image != p.Image || report.Operation != "watermark" || report.Applied != (p.Opacity > 0) || report.Width != source.Canvas.Width || report.Height != source.Canvas.Height || !finiteSigningNumber(report.MaxDelta, 0, 1) || report.Warnings == nil || !slices.Equal(report.Warnings, warnings) || p.Opacity == 0 && report.MaxDelta != 0 {
		return zero, ErrInvalid
	}
	// Both image ancestries participate, even with zero opacity. A canonical
	// producer may own both images; repeated task parents are deduplicated.
	content := []v2input.Ref{p.Image}
	if p.Watermark != p.Image {
		content = append(content, p.Watermark)
	}
	origins := map[v2input.Ref]uuid.UUID{}
	parents := []uuid.UUID{}
	for _, ref := range content {
		producer, err := s.Producers.ResolveSigningImageProducer(ctx, who, ready, ref)
		if err != nil {
			return zero, err
		}
		if producer == uuid.Nil || producer == e.Task {
			return zero, ErrInvalid
		}
		origins[ref] = producer
		if !slices.Contains(parents, producer) {
			parents = append(parents, producer)
		}
	}
	for ref, raw := range documents {
		current, err := s.Documents.ReadSigningTaskDocument(ctx, who, ready, e, ref, 1<<20)
		if err != nil {
			return zero, err
		}
		if !bytes.Equal(current, raw) {
			return zero, ErrInvalid
		}
	}
	for ref, producer := range origins {
		current, err := s.Producers.ResolveSigningImageProducer(ctx, who, ready, ref)
		if err != nil {
			return zero, err
		}
		if current != producer {
			return zero, ErrInvalid
		}
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	sort.Slice(parents, func(i, j int) bool { return parents[i].String() < parents[j].String() })
	return ResolvedSigningTask{Node: SigningExecutionNode{Task: e.Task, Run: e.Run, CanonicalResult: e.Result, Attempt: e.Attempt, Fence: e.Fence, Parents: parents, Inputs: content, Outputs: outputs, Action: SigningAction{Operation: e.Operation, Version: e.Version, ParamsSHA: e.Params.SHA256}}}, nil
}

func canonicalTaskInputRefs(raw []byte) ([]v2input.Ref, error) {
	var refs []v2input.Ref
	if len(raw) < 2 || len(raw) > 16<<20 || json.Unmarshal(raw, &refs) != nil || len(refs) < 1 || len(refs) > 30000 {
		return nil, ErrInvalid
	}
	encoded, err := canonical.Marshal(refs)
	if err != nil || !bytes.Equal(encoded, raw) {
		return nil, ErrInvalid
	}
	for _, r := range refs {
		if !ref(r) {
			return nil, ErrInvalid
		}
	}
	return refs, nil
}
func decodeSigningWatermarkParams(raw []byte) (signingWatermarkParams, error) {
	var p signingWatermarkParams
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
	for key, value := range map[string]json.RawMessage{"margin_x": json.RawMessage(`0.03`), "margin_y": json.RawMessage(`0.03`), "max_width_fraction": json.RawMessage(`0.25`), "algorithm": json.RawMessage(`"linear_alpha_v1"`)} {
		if _, ok := fields[key]; !ok {
			fields[key] = value
		}
	}
	normalized, err = canonical.Marshal(fields)
	if err != nil || decode(normalized, &p, 1<<20) != nil || !ref(p.Image) || !ref(p.Watermark) || p.Algorithm != "linear_alpha_v1" || !finiteSigningNumber(p.Opacity, 0, 1) || !finiteSigningNumber(p.MarginX, 0, 1) || !finiteSigningNumber(p.MarginY, 0, 1) || !finiteSigningNumber(p.MaxWidth, 0, 1) || p.MaxWidth == 0 {
		return signingWatermarkParams{}, ErrInvalid
	}
	if !slices.Contains([]string{"top_left", "top_right", "bottom_left", "bottom_right", "center"}, p.Anchor) {
		return signingWatermarkParams{}, ErrInvalid
	}
	return p, nil
}
func signingWatermarkFits(source, mark review.Canvas, p signingWatermarkParams) bool {
	scale := math.Min(1, float64(source.Width)*p.MaxWidth/float64(mark.Width))
	w, h := max(int64(1), int64(math.RoundToEven(float64(mark.Width)*scale))), max(int64(1), int64(math.RoundToEven(float64(mark.Height)*scale)))
	mx, my := int64(math.RoundToEven(p.MarginX*float64(source.Width))), int64(math.RoundToEven(p.MarginY*float64(source.Height)))
	x, y := mx, my
	switch p.Anchor {
	case "top_right":
		x = source.Width - w - mx
	case "bottom_left":
		y = source.Height - h - my
	case "bottom_right":
		x = source.Width - w - mx
		y = source.Height - h - my
	case "center":
		x = (source.Width - w) / 2
		y = (source.Height - h) / 2
	}
	return x >= 0 && y >= 0 && x+w <= source.Width && y+h <= source.Height
}

var _ SigningTaskLineageResolver = WatermarkSigningTaskResolver{}
