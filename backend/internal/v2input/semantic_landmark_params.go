package v2input

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
)

type SemanticLandmarkPreparation struct {
	Params json.RawMessage
	Inputs []Entry
}

// Binds FAN2D inputs to exact original SFD Params and detector output bytes.
// Canonical producer/attempt, scientific model qualifications, deployed model
// identity, GPU budgets, publication and locked admission remain caller duties.
func PrepareSemanticLandmarkParams(ctx context.Context, binding SessionVersionBinding, imageBody json.RawMessage, faces Ref, facesBody json.RawMessage, detectorParams Ref, detectorBody json.RawMessage, modelLock string) (SemanticLandmarkPreparation, error) {
	var zero SemanticLandmarkPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if !validRef(faces) || !validRef(detectorParams) || faces.ArtifactID == detectorParams.ArtifactID || len(facesBody) < 2 || len(facesBody) > 1<<20 || len(detectorBody) < 2 || len(detectorBody) > 1<<20 {
		return zero, ErrInvalid
	}
	if _, err := domain.ParseDigest(modelLock); err != nil {
		return zero, ErrInvalid
	}
	sum := sha256.Sum256(facesBody)
	if hex.EncodeToString(sum[:]) != faces.SHA256 {
		return zero, ErrInvalid
	}
	inputs, err := prepareSemanticFaceSourceInputs(ctx, binding, imageBody)
	if err != nil {
		return zero, err
	}
	for _, entry := range inputs {
		if entry.Ref.ArtifactID == faces.ArtifactID || entry.Ref.ArtifactID == detectorParams.ArtifactID {
			return zero, ErrInvalid
		}
	}
	image, err := review.ParseWorkingImage(imageBody)
	if err != nil {
		return zero, ErrInvalid
	}
	source := image.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256})
	if err := review.VerifyFaceDetectParams(review.ArtifactRef{ID: detectorParams.ArtifactID, SHA256: detectorParams.SHA256}, detectorBody, facesBody, source); err != nil {
		return zero, err
	}
	params, err := canonical.Marshal(review.LandmarkParams{Image: source.Image, Faces: review.ArtifactRef{ID: faces.ArtifactID, SHA256: faces.SHA256}, ModelID: "fan2d", ModelLockID: modelLock, PreprocessingID: "fan2d_crop256_v1", CropSize: 256, Expansion: 1.25})
	if err != nil {
		return zero, ErrInvalid
	}
	inputs = append(inputs, Entry{Ref: faces, Contract: "FaceBoxes"})
	sort.Slice(inputs, func(i, j int) bool {
		return bytes.Compare(inputs[i].Ref.ArtifactID[:], inputs[j].Ref.ArtifactID[:]) < 0
	})
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return SemanticLandmarkPreparation{Params: params, Inputs: inputs}, nil
}
