package v2portrait

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// CompileLocalExecutionProfile binds exact source bytes to conservative CPU
// resources and an operator-selected runtime pin. The caller must establish
// source authority, runtime qualification and actual host/quota capacities.
func CompileLocalExecutionProfile(binding v2input.SessionVersionBinding, sourceRaw []byte,
	runtimeSHA string, capacity LocalResourceEstimate) (json.RawMessage, error) {
	invalid := func() (json.RawMessage, error) { return nil, v2input.ErrInvalid }
	if binding.AssetVersionID == uuid.Nil || binding.Source.ArtifactID == uuid.Nil || len(sourceRaw) < 2 || len(sourceRaw) > 1048576 {
		return invalid()
	}
	if _, err := domain.ParseDigest(runtimeSHA); err != nil {
		return invalid()
	}
	sum := sha256.Sum256(sourceRaw)
	if hex.EncodeToString(sum[:]) != binding.Source.SHA256 {
		return invalid()
	}
	source, err := review.ParseWorkingImage(sourceRaw)
	if err != nil || source.DType != "float32" {
		return invalid()
	}
	canvasHash, err := canonical.Hash(source.Canvas)
	if err != nil || canvasHash != binding.CanvasHash {
		return invalid()
	}
	resources, err := LocalPortraitResources(source.Canvas.Width, source.Canvas.Height)
	if err != nil {
		return invalid()
	}
	for _, n := range []int64{capacity.PixelCount, capacity.RAMBytes, capacity.ScratchBytes, capacity.OutputBytes, capacity.VRAMBytes} {
		if n < 0 || n > 9007199254740991 {
			return invalid()
		}
	}
	if capacity.PixelCount < resources.PixelCount || capacity.RAMBytes < resources.RAMBytes || capacity.ScratchBytes < resources.ScratchBytes || capacity.OutputBytes < resources.OutputBytes {
		return invalid()
	}
	return canonical.Marshal(struct {
		Backend    string                `json:"backend"`
		Renderer   string                `json:"renderer_version"`
		RuntimeSHA string                `json:"portrait_runtime_sha"`
		Resources  LocalResourceEstimate `json:"resource_estimate"`
	}{"cpu", "v2.2", runtimeSHA, resources})
}
