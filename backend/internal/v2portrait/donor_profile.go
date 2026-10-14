package v2portrait

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func VerifyDonorWorkingImage(binding v2input.SessionVersionBinding, raw []byte) (review.WorkingImage, error) {
	invalid := func() (review.WorkingImage, error) { return review.WorkingImage{}, v2input.ErrInvalid }
	if binding.AssetVersionID == uuid.Nil || binding.Source.ArtifactID == uuid.Nil || len(raw) < 2 || len(raw) > 1048576 {
		return invalid()
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != binding.Source.SHA256 {
		return invalid()
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil || image.DType != "float32" || image.Pixels.ID == uuid.Nil ||
		image.Alpha != nil && image.Alpha.ID == uuid.Nil {
		return invalid()
	}
	normalized, err := canonical.Marshal(image)
	if err != nil || !bytes.Equal(normalized, raw) {
		return invalid()
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil || canvas != binding.CanvasHash {
		return invalid()
	}
	if _, err = LocalPortraitResources(image.Canvas.Width, image.Canvas.Height); err != nil {
		return invalid()
	}
	return image, nil
}

// CompileDonorExecutionProfile requires caller-established artifact, runtime,
// patch/halo authority and actual capacity. This pure compiler does not admit a task.
func CompileDonorExecutionProfile(params OpenEyesParams, sourceRaw, donorRaw []byte,
	patchWidth, patchHeight int64, runtimeSHA string, capacity DonorResourceEstimate) (json.RawMessage, error) {
	invalid := func() (json.RawMessage, error) { return nil, v2input.ErrInvalid }
	if _, _, err := params.Compile(); err != nil {
		return invalid()
	}
	if _, err := domain.ParseDigest(runtimeSHA); err != nil {
		return invalid()
	}
	source, err := VerifyDonorWorkingImage(params.Base, sourceRaw)
	if err != nil {
		return invalid()
	}
	donor, err := VerifyDonorWorkingImage(params.Donor, donorRaw)
	if err != nil || source.Color != donor.Color {
		return invalid()
	}
	resources, err := DonorPortraitResources(source.Canvas.Width, source.Canvas.Height,
		donor.Canvas.Width, donor.Canvas.Height, patchWidth, patchHeight)
	if err != nil {
		return invalid()
	}
	have := []int64{capacity.SourcePixelCount, capacity.DonorPixelCount, capacity.PatchPixelCount,
		capacity.RAMBytes, capacity.MaskScratchBytes, capacity.MaterialScratchBytes, capacity.OutputScratchBytes,
		capacity.ScratchBytes, capacity.OutputBytes, capacity.VRAMBytes}
	need := []int64{resources.SourcePixelCount, resources.DonorPixelCount, resources.PatchPixelCount,
		resources.RAMBytes, resources.MaskScratchBytes, resources.MaterialScratchBytes, resources.OutputScratchBytes,
		resources.ScratchBytes, resources.OutputBytes, resources.VRAMBytes}
	for i, n := range have {
		if n < need[i] || n > 9007199254740991 {
			return invalid()
		}
	}
	return canonical.Marshal(struct {
		Backend    string                `json:"backend"`
		Renderer   string                `json:"renderer_version"`
		RuntimeSHA string                `json:"portrait_runtime_sha"`
		Resources  DonorResourceEstimate `json:"resource_estimate"`
	}{"cpu", "v2.2", runtimeSHA, resources})
}
