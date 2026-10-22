package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type authoredPreparationUpscaleParams struct {
	Image   v2input.Ref `json:"image"`
	Scale   int         `json:"scale"`
	Model   string      `json:"model_id"`
	Lock    string      `json:"model_lock_id"`
	Adapter string      `json:"color_adapter_id"`
	Tile    int         `json:"tile"`
	Overlap int         `json:"overlap"`
}

// validatePreparationUpscaleGeometry mirrors the actual prepare_upscale geometry
// entry, not an invented resize or a blend-target projection. Model delta and
// photographic replay remain the independent native-adopter obligation.
func validatePreparationUpscaleGeometry(phase AuthoredPhaseReceipt, paramsRaw, sourceRaw, imageRaw, reportRaw []byte) (v2draft.DocumentGeometry, error) {
	empty := v2draft.DocumentGeometry{}
	fail := func() (v2draft.DocumentGeometry, error) { return empty, v2input.ErrInvalid }
	if phase.Geometry == nil || phase.Geometry.Task.Operation != "image.upscale" || phase.Geometry.Root != phase.Geometry.Image {
		return fail()
	}
	var p authoredPreparationUpscaleParams
	var keys map[string]json.RawMessage
	if json.Unmarshal(paramsRaw, &keys) != nil || len(keys) != 7 || decodeAuthoredParams(paramsRaw, &p) != nil || p.Image != phase.InputSource.Source || ((p.Scale != 2 || p.Model != "realesrgan_x2") && (p.Scale != 4 || p.Model != "realesrgan_x4")) || p.Adapter != "work_srgb_delta_v1" || p.Tile != 256 || p.Overlap != 32 {
		return fail()
	}
	if _, err := domain.ParseDigest(p.Lock); err != nil {
		return fail()
	}
	source, err := review.ParseWorkingImage(sourceRaw)
	if err != nil {
		return fail()
	}
	image, err := review.ParseWorkingImage(imageRaw)
	if err != nil {
		return fail()
	}
	sourceSHA, err := canonical.Hash(source)
	canvasSHA, e := canonical.Hash(source.Canvas)
	if err != nil || e != nil || sourceSHA != p.Image.SHA256 || canvasSHA != phase.InputSource.CanvasHash || source.DType != "float32" || source.Color.ProfileID != "work_rec2020_gamma24_v1" || source.Color.Encoding != "gamma24" {
		return fail()
	}
	width, height := source.Canvas.Width*int64(p.Scale), source.Canvas.Height*int64(p.Scale)
	if width < 1 || height < 1 || width > 200000000/height {
		return fail()
	}
	expected := review.Canvas{Width: width, Height: height, Orientation: 1, SpaceID: fmt.Sprintf("up%d-%s", p.Scale, source.Canvas.SourceSHA256[:16]), SourceSHA256: source.Canvas.SourceSHA256}
	if image.Canvas != expected || image.Color != source.Color || image.DType != "float32" || (image.Alpha == nil) != (source.Alpha == nil) || image.Pixels == source.Pixels {
		return fail()
	}
	if source.Alpha != nil && (image.Pixels == *source.Alpha || *image.Alpha == *source.Alpha || *image.Alpha == source.Pixels || *image.Alpha == image.Pixels) {
		return fail()
	}
	var before, after struct {
		Steps []json.RawMessage `json:"steps"`
		Hash  string            `json:"hash"`
	}
	if json.Unmarshal(source.TransformChain, &before) != nil || json.Unmarshal(image.TransformChain, &after) != nil || len(after.Steps) != len(before.Steps)+1 {
		return fail()
	}
	for i, raw := range before.Steps {
		a, e := canonical.Bytes(raw)
		b, e2 := canonical.Bytes(after.Steps[i])
		if e != nil || e2 != nil || !bytes.Equal(a, b) {
			return fail()
		}
	}
	step, err := canonical.Marshal(map[string]any{"kind": "resize", "from_canvas": source.Canvas, "to_canvas": expected, "invertibility": "invertible", "valid_region": map[string]int64{"x": 0, "y": 0, "width": width, "height": height}, "pixel_centers": "half_pixel"})
	actual, err2 := canonical.Bytes(after.Steps[len(after.Steps)-1])
	if err != nil || err2 != nil || !bytes.Equal(step, actual) {
		return fail()
	}
	var report struct {
		Image      v2input.Ref `json:"image"`
		Model      string      `json:"model_id"`
		Lock       string      `json:"model_lock_id"`
		Backend    string      `json:"backend"`
		Scale      int         `json:"scale"`
		Adapter    string      `json:"color_adapter_id"`
		Tile       int         `json:"tile"`
		Overlap    int         `json:"overlap"`
		Input      int64       `json:"input_pixel_count"`
		Output     int64       `json:"output_pixel_count"`
		Disabled   float64     `json:"disabled_delta_fraction"`
		SourceClip float64     `json:"source_clip_fraction"`
		Gamut      float64     `json:"gamut_excess_fraction"`
		Alpha      bool        `json:"alpha_resampled"`
		Warnings   []string    `json:"warnings"`
	}
	keys = nil
	if json.Unmarshal(reportRaw, &keys) != nil || len(keys) != 15 || decodeAuthoredParams(reportRaw, &report) != nil {
		return fail()
	}
	var profile struct {
		Backend string `json:"backend"`
	}
	if json.Unmarshal([]byte(phase.Geometry.Task.ProfileJCS), &profile) != nil {
		return fail()
	}
	if report.Image != p.Image || report.Model != p.Model || report.Lock != p.Lock || report.Scale != p.Scale || report.Adapter != p.Adapter || report.Tile != p.Tile || report.Overlap != p.Overlap || report.Input != source.Canvas.Width*source.Canvas.Height || report.Output != width*height || report.Alpha != (image.Alpha != nil) || report.Backend != profile.Backend {
		return fail()
	}
	if report.Backend != "cpu" && report.Backend != "cuda" && report.Backend != "mps" {
		return fail()
	}
	for _, value := range []float64{report.Disabled, report.SourceClip, report.Gamut} {
		if value < 0 || value > 1 {
			return fail()
		}
	}
	seen := map[string]bool{}
	for _, warning := range report.Warnings {
		if seen[warning] || (warning != "source_srgb_clipped" && warning != "delta_gamut_excess") {
			return fail()
		}
		seen[warning] = true
	}
	if report.Warnings == nil {
		return fail()
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil {
		return empty, err
	}
	chain, err := canonical.Hash(image.TransformChain)
	if err != nil {
		return empty, err
	}
	return v2draft.DocumentGeometry{CanvasHash: canvas, TransformHash: chain}, nil
}
func (s SQLAuthoredGeometryInputCatalog) nativeUpscaleGeometry(ctx context.Context, who auth.Identity, tenant uuid.UUID, phase AuthoredPhaseReceipt) (v2draft.DocumentGeometry, error) {
	empty := v2draft.DocumentGeometry{}
	if phase.Geometry == nil {
		return empty, v2input.ErrInvalid
	}
	n := phase.Geometry
	refs := []v2input.Ref{n.Task.Params, phase.InputSource.Source, n.Image, n.Report}
	raw := make([][]byte, len(refs))
	for i, ref := range refs {
		var err error
		raw[i], err = s.Objects.ReadJSON(ctx, who, tenant, s.Project, s.Parent, s.Ordinal, ref)
		if err != nil {
			return empty, err
		}
	}
	return validatePreparationUpscaleGeometry(phase, raw[0], raw[1], raw[2], raw[3])
}
