package provenance

import (
	"bytes"
	"context"
	"math"
	"slices"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type signingSharpenParams struct {
	Image     v2input.Ref `json:"image"`
	Radius    float64     `json:"radius"`
	Strength  float64     `json:"strength"`
	Threshold float64     `json:"threshold"`
	MaxDelta  float64     `json:"max_delta"`
	Algorithm string      `json:"algorithm"`
}
type signingDerivativeReport struct {
	Image     v2input.Ref `json:"image"`
	Operation string      `json:"operation"`
	Applied   bool        `json:"applied"`
	Width     int64       `json:"width"`
	Height    int64       `json:"height"`
	MaxDelta  float64     `json:"max_channel_delta"`
	Warnings  []string    `json:"warnings"`
}
type SharpenSigningTaskResolver struct {
	Documents SigningTaskDocumentReader
	Producers SigningImageProducer
}

func (s SharpenSigningTaskResolver) ResolveSigningTaskLineage(ctx context.Context, who auth.Identity, ready ReadySigningExport, e CanonicalSigningTask) (ResolvedSigningTask, error) {
	var zero ResolvedSigningTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Documents == nil || s.Producers == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || ready.Tenant == uuid.Nil || ready.Project == uuid.Nil || ready.ExportID == uuid.Nil || e.Operation != "derivative.sharpen" || e.Version != "1" || e.ReviewRequired || normalizeCanonicalSigningTask(&e) != nil {
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
	var p signingSharpenParams
	if decode(paramsRaw, &p, 1<<20) != nil || !ref(p.Image) || p.Algorithm != "luma_unsharp_v1" || !finiteSigningNumber(p.Radius, .1, 5) || !finiteSigningNumber(p.Strength, 0, 1) || !finiteSigningNumber(p.Threshold, 0, .1) || !finiteSigningNumber(p.MaxDelta, 0, .03) || p.MaxDelta == 0 {
		return zero, ErrInvalid
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
	resultRef, reportRef := outputs[len(outputs)-2], outputs[len(outputs)-1]
	resultRaw, err := read(resultRef)
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
	reportRaw, err := read(reportRef)
	if err != nil {
		return zero, err
	}
	var report signingDerivativeReport
	if decode(reportRaw, &report, 1<<20) != nil {
		return zero, ErrInvalid
	}
	warnings := []string{}
	if p.Strength == 0 {
		warnings = []string{"zero_strength"}
	}
	if report.Image != p.Image || report.Operation != "sharpen" || report.Applied != (p.Strength > 0) || report.Width != source.Canvas.Width || report.Height != source.Canvas.Height || !finiteSigningNumber(report.MaxDelta, 0, math.Min(1, p.MaxDelta+1e-6)) || report.Warnings == nil || !slices.Equal(report.Warnings, warnings) || p.Strength == 0 && report.MaxDelta != 0 {
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

func finiteSigningNumber(value, minimum, maximum float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= minimum && value <= maximum
}

var _ SigningTaskLineageResolver = SharpenSigningTaskResolver{}
