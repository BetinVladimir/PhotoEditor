package v2optical

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func VerifyNegativeWorkingImage(params NegativeParams, raw []byte) (review.WorkingImage, error) {
	invalid := func() (review.WorkingImage, error) { return review.WorkingImage{}, v2input.ErrInvalid }
	if _, _, err := params.Compile(); err != nil {
		return invalid()
	}
	if len(raw) < 2 || len(raw) > 65536 {
		return invalid()
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != params.Base.Source.SHA256 {
		return invalid()
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil || image.DType != "float32" || image.Color.Encoding != "linear" || image.Pixels.ID == uuid.Nil || image.Alpha != nil && image.Alpha.ID == uuid.Nil {
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
	if _, err := NegativeResources(image.Canvas.Width, image.Canvas.Height, image.Alpha != nil); err != nil {
		return invalid()
	}
	return image, nil
}

// CompileNegativeExecutionProfile verifies original metadata and owned content
// evidence. Current SQL/source/object authority and graph sealing are separate.
func CompileNegativeExecutionProfile(params NegativeParams, sourceRaw []byte,
	qualified VerifiedNegativeQualification, capacity NegativeResourceEstimate) (json.RawMessage, error) {
	invalid := func() (json.RawMessage, error) { return nil, v2input.ErrInvalid }
	image, err := VerifyNegativeWorkingImage(params, sourceRaw)
	if err != nil {
		return invalid()
	}
	pin, err := qualified.Pin()
	if err != nil {
		return nil, err
	}
	if qualified.params != params || qualified.inputICC != image.Color.ICCSHA256 {
		return invalid()
	}
	required, err := NegativeResources(image.Canvas.Width, image.Canvas.Height, image.Alpha != nil)
	if err != nil {
		return invalid()
	}
	if capacity.PixelCount != required.PixelCount || capacity.VRAMBytes != 0 {
		return invalid()
	}
	have := []int64{capacity.RAMBytes, capacity.MaterialScratchBytes, capacity.OutputScratchBytes, capacity.ScratchBytes, capacity.OutputBytes}
	need := []int64{required.RAMBytes, required.MaterialScratchBytes, required.OutputScratchBytes, required.ScratchBytes, required.OutputBytes}
	for i, n := range have {
		if n < need[i] || n > 9007199254740991 {
			return invalid()
		}
	}
	if capacity.ScratchBytes < capacity.MaterialScratchBytes+capacity.OutputScratchBytes {
		return invalid()
	}
	return canonical.Marshal(struct {
		Backend       string                   `json:"backend"`
		Renderer      string                   `json:"renderer_version"`
		RuntimeSHA    string                   `json:"negative_runtime_sha"`
		Qualification NegativeQualificationPin `json:"negative_qualification"`
		Resources     NegativeResourceEstimate `json:"resource_estimate"`
	}{"cpu", "v2.2", pin.RuntimeLockSHA, pin, required})
}
