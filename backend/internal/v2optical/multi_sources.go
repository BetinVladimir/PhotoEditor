package v2optical

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// VerifyMultiWorkingImages owns ordered metadata. SQL/object/ICC authority is separate.
func VerifyMultiWorkingImages(operation string, paramsRaw []byte, originals [][]byte) ([]review.WorkingImage, error) {
	var frames []v2input.SessionVersionBinding
	switch operation {
	case "optical.panorama":
		p, err := ParsePanoramaParams(paramsRaw)
		if err != nil {
			return nil, err
		}
		frames = p.Frames
	case "optical.focus_stack":
		p, err := ParseFocusStackParams(paramsRaw)
		if err != nil {
			return nil, err
		}
		frames = p.Frames
	default:
		return nil, v2input.ErrInvalid
	}
	if len(originals) != len(frames) {
		return nil, v2input.ErrInvalid
	}
	images := make([]review.WorkingImage, len(frames))
	for i, raw := range originals {
		if len(raw) < 2 || len(raw) > PerspectiveWorkingImageMaxBytes {
			return nil, v2input.ErrInvalid
		}
		digest := sha256.Sum256(raw)
		if hex.EncodeToString(digest[:]) != frames[i].Source.SHA256 {
			return nil, v2input.ErrInvalid
		}
		image, err := review.ParseWorkingImage(raw)
		if err != nil || image.DType != "float32" {
			return nil, v2input.ErrInvalid
		}
		body, err := canonical.Marshal(image)
		if err != nil || !bytes.Equal(body, raw) {
			return nil, v2input.ErrInvalid
		}
		canvas, err := canonical.Hash(image.Canvas)
		if err != nil || canvas != frames[i].CanvasHash || image.Canvas.Width < 8 || image.Canvas.Height < 8 ||
			image.Canvas.Width > 200000000/image.Canvas.Height {
			return nil, v2input.ErrInvalid
		}
		if image.Color.ProfileID != "linear_srgb_v1" && image.Color.ProfileID != "work_rec2020_gamma24_v1" {
			return nil, v2input.ErrInvalid
		}
		if (image.Color.ProfileID == "linear_srgb_v1" && image.Color.Encoding != "linear") ||
			(image.Color.ProfileID == "work_rec2020_gamma24_v1" && image.Color.Encoding != "gamma24") {
			return nil, v2input.ErrInvalid
		}
		if i > 0 && image.Color != images[0].Color {
			return nil, v2input.ErrInvalid
		}
		images[i] = image
	}
	return images, nil
}
