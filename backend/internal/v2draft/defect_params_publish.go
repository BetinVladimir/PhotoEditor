package v2draft

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"photoeditor/backend/internal/canonical"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Publishes native compiler Params as an accepted-version scoped document,
// using existing draft quota, fencing and cleanup. Source/producer qualification
// and final task admission remain preparer responsibilities.
func (p Publisher) PublishDefectProposalParams(ctx context.Context, who auth.Identity, tenant, document uuid.UUID, binding v2input.SessionVersionBinding, imageBody, paramsBody []byte, inputs []review.ArtifactRef) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	invalid := func() (v2input.Ref, error) { return v2input.Ref{}, domain.NewError("VALIDATION_FAILED", 422) }
	if tenant == uuid.Nil || document == uuid.Nil || binding.AssetVersionID == uuid.Nil || binding.Source.ArtifactID == uuid.Nil || len(imageBody) < 2 || len(imageBody) > 1048576 {
		return invalid()
	}
	imageDigest := sha256.Sum256(imageBody)
	if binding.Source.SHA256 != hex.EncodeToString(imageDigest[:]) {
		return invalid()
	}
	image, err := review.ParseWorkingImage(imageBody)
	if err != nil || image.DType != "float32" || image.Color.ProfileID != "work_rec2020_gamma24_v1" {
		return invalid()
	}
	metadata, err := v2input.ResolveSemanticSourceMetadata(binding, imageBody)
	if err != nil || metadata.Inputs.Pixels.ArtifactID != image.Pixels.ID {
		return invalid()
	}
	canonicalParams, err := canonical.Bytes(paramsBody)
	if err != nil || !bytes.Equal(canonicalParams, paramsBody) {
		return invalid()
	}
	paramsDigest := sha256.Sum256(paramsBody)
	source := image.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256})
	if err := review.VerifyDefectProposalDeclaredInputs(review.ArtifactRef{ID: document, SHA256: hex.EncodeToString(paramsDigest[:])}, paramsBody, source, inputs); err != nil {
		return invalid()
	}
	return p.publish(ctx, who, tenant, binding.AssetVersionID, document, paramsBody, false, true)
}
