package v2optical

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// PerspectiveWorkingImageMaxBytes bounds original WorkingImage JSON, not Params or NPY headers.
const PerspectiveWorkingImageMaxBytes = 1048576

func VerifyPerspectiveWorkingImage(params PerspectiveParams, raw []byte) (review.WorkingImage, error) {
	invalid := func() (review.WorkingImage, error) { return review.WorkingImage{}, v2input.ErrInvalid }
	if _, _, err := params.Compile(); err != nil {
		return invalid()
	}
	if len(raw) < 2 || len(raw) > PerspectiveWorkingImageMaxBytes {
		return invalid()
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != params.Base.Source.SHA256 {
		return invalid()
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil || image.DType != "float32" || image.Pixels.ID == uuid.Nil || image.Alpha != nil && image.Alpha.ID == uuid.Nil {
		return invalid()
	}
	body, err := canonical.Marshal(image)
	if err != nil || !bytes.Equal(body, raw) {
		return invalid()
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil || canvas != params.Base.CanvasHash {
		return invalid()
	}
	if _, err := PerspectiveResources(image.Canvas.Width, image.Canvas.Height, image.Canvas.Width, image.Canvas.Height); err != nil {
		return invalid()
	}
	return image, nil
}
