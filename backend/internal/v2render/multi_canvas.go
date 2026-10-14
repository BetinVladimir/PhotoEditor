package v2render

import (
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Multi output canvas identity is content-derived using the same seed as the
// worker. All source occurrences and normalized registration matrices matter.
func multiCanvasSpace(binding AdoptedMultiBinding, width, height int64, registrations []MultiRegistration) (string, error) {
	frames, reference, _, err := binding.Inputs()
	if err != nil || len(registrations) != len(frames) || width < 1 || height < 1 || width > 200000000/height {
		return "", v2input.ErrInvalid
	}
	matrices := make([][]float64, len(frames))
	fields := make([]*v2input.Ref, len(frames))
	for i, r := range registrations {
		if r.Source != frames[i] || len(r.Homography) != 9 || r.Homography[8] != 1 {
			return "", v2input.ErrInvalid
		}
		matrices[i] = r.Homography
		fields[i] = r.InverseField
	}
	seed := struct {
		Operation     string                          `json:"operation"`
		Reference     int                             `json:"reference"`
		Sources       []v2input.SessionVersionBinding `json:"sources"`
		Matrices      [][]float64                     `json:"matrices"`
		InverseFields []*v2input.Ref                  `json:"inverse_fields"`
		Height        int64                           `json:"height"`
		Width         int64                           `json:"width"`
	}{binding.Operation, reference, frames, matrices, fields, height, width}
	sha, err := canonical.Hash(seed)
	if err != nil {
		return "", err
	}
	return "multi:" + sha[:32], nil
}
func multiProjectionCanvas(source review.Canvas, field v2input.Ref) (review.Canvas, error) {
	sha, err := canonical.Hash(struct {
		Canvas review.Canvas `json:"canvas"`
		Field  v2input.Ref   `json:"field"`
	}{source, field})
	if err != nil {
		return review.Canvas{}, err
	}
	source.Orientation = 1
	source.SpaceID = "cylindrical:" + sha[:32]
	return source, nil
}
