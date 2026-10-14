package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2optical"
)

// ValidateMultiOutputGeometry binds typed output geometry to ordered, physically
// verified originals. It does not replay registration, blending or image quality.
func ValidateMultiOutputGeometry(source VerifiedMultiSourceClosure, imageRaw, maskRaw []byte, registrations [][]byte) (review.WorkingImage, review.MaskRef, []MultiRegistration, error) {
	invalid := func() (review.WorkingImage, review.MaskRef, []MultiRegistration, error) {
		return review.WorkingImage{}, review.MaskRef{}, nil, v2input.ErrInvalid
	}
	binding, err := source.Binding()
	if err != nil {
		return invalid()
	}
	originals, err := source.Images()
	if err != nil {
		return invalid()
	}
	frames, reference, calibration, err := binding.Inputs()
	if err != nil || len(registrations) != len(frames) {
		return invalid()
	}
	image, err := review.ParseWorkingImage(imageRaw)
	if err != nil {
		return invalid()
	}
	mask, err := review.ParseMaskRef(maskRaw)
	if err != nil {
		return invalid()
	}
	// Require exact canonical contracts, including explicit nullable fields.
	exact, err := canonical.Marshal(image)
	if err != nil || !bytes.Equal(exact, imageRaw) {
		return invalid()
	}
	exact, err = canonical.Marshal(mask)
	if err != nil || !bytes.Equal(exact, maskRaw) {
		return invalid()
	}
	ref := originals[reference]
	if image.DType != "float32" || image.Alpha == nil || image.Color != ref.Color || image.Canvas.Orientation != 1 || image.Canvas.SourceSHA256 != ref.Canvas.SourceSHA256 || mask.Canvas != image.Canvas || !bytes.Equal(mask.TransformChain, image.TransformChain) || mask.Selector != "changed" || mask.Confidence != 1 || mask.Protected || mask.ObjectID != nil || mask.ModelLockID != nil {
		return invalid()
	}
	var maximum int64
	nonlinear := false
	if binding.Operation == "optical.panorama" {
		p, e := v2optical.ParsePanoramaParams([]byte(binding.ParamsJCS))
		if e != nil {
			return invalid()
		}
		maximum = int64(p.MaxPixels)
		nonlinear = p.Projection == "cylindrical"
	} else {
		p, e := v2optical.ParseFocusStackParams([]byte(binding.ParamsJCS))
		if e != nil {
			return invalid()
		}
		maximum = int64(p.MaxPixels)
		if image.Canvas.Width != ref.Canvas.Width || image.Canvas.Height != ref.Canvas.Height {
			return invalid()
		}
	}
	if image.Canvas.Height < 1 || image.Canvas.Width > maximum/image.Canvas.Height {
		return invalid()
	}
	target, err := canonical.Hash(image.Canvas)
	if err != nil {
		return invalid()
	}
	parsed := make([]MultiRegistration, len(frames))
	for i, raw := range registrations {
		parsed[i], err = ParseMultiRegistration(raw, frames[i], target, nonlinear)
		if err == nil {
			err = v2optical.VerifyMultiHomographySafety(context.Background(), parsed[i].Homography, originals[i].Canvas.Width, originals[i].Canvas.Height)
		}
		if err != nil {
			return invalid()
		}
	}
	expectedSpace, err := multiCanvasSpace(binding, image.Canvas.Width, image.Canvas.Height, parsed)
	if err != nil || image.Canvas.SpaceID != expectedSpace {
		return invalid()
	}
	type chain struct {
		Steps []json.RawMessage `json:"steps"`
		Hash  string            `json:"hash"`
	}
	var before, after chain
	if json.Unmarshal(ref.TransformChain, &before) != nil || json.Unmarshal(image.TransformChain, &after) != nil {
		return invalid()
	}
	extra := 1
	if nonlinear {
		extra = 2
	}
	if len(after.Steps) != len(before.Steps)+extra {
		return invalid()
	}
	for i, step := range before.Steps {
		if !bytes.Equal(step, after.Steps[i]) {
			return invalid()
		}
	}
	type step struct {
		Kind    string        `json:"kind"`
		From    review.Canvas `json:"from_canvas"`
		To      review.Canvas `json:"to_canvas"`
		Matrix  []float64     `json:"forward_matrix"`
		Field   *v2input.Ref  `json:"inverse_field"`
		Centers string        `json:"pixel_centers"`
	}
	from := ref.Canvas
	if nonlinear {
		var projection step
		if json.Unmarshal(after.Steps[len(before.Steps)], &projection) != nil || projection.Kind != "dense_warp" || projection.From != from || projection.Field == nil || parsed[reference].InverseField == nil || *projection.Field != *parsed[reference].InverseField || projection.Centers != "half_pixel" || projection.To.Width != from.Width || projection.To.Height != from.Height || projection.To.SourceSHA256 != from.SourceSHA256 {
			return invalid()
		}
		expectedProjection, err := multiProjectionCanvas(ref.Canvas, *parsed[reference].InverseField)
		if err != nil || projection.To != expectedProjection {
			return invalid()
		}
		from = projection.To
	}
	var final step
	if json.Unmarshal(after.Steps[len(after.Steps)-1], &final) != nil || final.Kind != "matrix" || final.From != from || final.To != image.Canvas || len(final.Matrix) != 9 {
		return invalid()
	}
	for i, value := range final.Matrix {
		if value != parsed[reference].Homography[i] {
			return invalid()
		}
	}
	used := map[string]bool{calibration.ArtifactID.String(): true}
	for _, item := range source.originals.inventory.items {
		used[item.Ref.ArtifactID.String()] = true
	}
	add := func(id string) bool {
		if used[id] {
			return false
		}
		used[id] = true
		return true
	}
	for _, original := range originals {
		used[original.Pixels.ID.String()] = true
		if original.Alpha != nil {
			used[original.Alpha.ID.String()] = true
		}
	}
	for _, frame := range frames {
		used[frame.Source.ArtifactID.String()] = true
	}
	used[binding.Result.ArtifactID.String()] = true
	if !add(image.Pixels.ID.String()) || !add(image.Alpha.ID.String()) || !add(mask.Artifact.ID.String()) {
		return invalid()
	}
	for _, r := range parsed {
		if r.InverseField != nil && !add(r.InverseField.ArtifactID.String()) {
			return invalid()
		}
	}
	return image, mask, parsed, nil
}
