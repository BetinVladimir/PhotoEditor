package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Verify actual original RGB/alpha/geometry and deployed ICC before Params
// publication; recheck the exact retained source afterward. Qualification and
// locked task admission remain separate. No SQL transaction spans object IO.
func (s SQLTransferSourceReader) publishSourceDetectorVersionParams(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, publisher v2draft.Publisher, document uuid.UUID, model string, score float64, maxFaces int) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if document == uuid.Nil || tenant == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || binding.AssetVersionID == uuid.Nil || binding.Source.ArtifactID == uuid.Nil || document == binding.Source.ArtifactID {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(model); err != nil {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	if math.IsNaN(score) || math.IsInf(score, 0) || score < .5 || score > .99 || maxFaces < 1 || maxFaces > 100 {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	_, proof, err := s.readQualifiedSourcePreparationProof(ctx, who, tenant, binding, color, true)
	if err != nil {
		return v2input.Ref{}, err
	}
	image, err := s.Read(ctx, who, tenant, binding)
	if err != nil {
		return v2input.Ref{}, err
	}
	prepared, err := v2input.PrepareSemanticFaceDetectParams(ctx, binding, image, model, score, maxFaces)
	if err != nil {
		return v2input.Ref{}, err
	}
	ref, err := publisher.PublishSemanticDetectorParams(ctx, who, tenant, document, binding, image, model, score, maxFaces)
	if err != nil {
		return v2input.Ref{}, err
	}
	digest, err := canonical.Hash(prepared.Params)
	if err != nil || ref.ArtifactID == uuid.Nil || ref.SHA256 != digest {
		return v2input.Ref{}, v2input.ErrConflict
	}
	if err := s.recheckSourcePreparationProof(ctx, who, tenant, binding, color, true, proof); err != nil {
		return v2input.Ref{}, err
	}
	return ref, nil
}
func (s SQLTransferSourceReader) publishSourcePersonVersionParams(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, publisher v2draft.Publisher, document uuid.UUID, model string, guidedTile int) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if document == uuid.Nil || tenant == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || binding.AssetVersionID == uuid.Nil || binding.Source.ArtifactID == uuid.Nil || document == binding.Source.ArtifactID {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(model); err != nil {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	if guidedTile != 256 && guidedTile != 512 && guidedTile != 1024 {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	_, proof, err := s.readQualifiedSourcePreparationProof(ctx, who, tenant, binding, color, true)
	if err != nil {
		return v2input.Ref{}, err
	}
	image, err := s.Read(ctx, who, tenant, binding)
	if err != nil {
		return v2input.Ref{}, err
	}
	prepared, err := v2input.PrepareSemanticPersonParams(ctx, binding, image, model, guidedTile)
	if err != nil {
		return v2input.Ref{}, err
	}
	ref, err := publisher.PublishSemanticPersonParams(ctx, who, tenant, document, binding, image, model, guidedTile)
	if err != nil {
		return v2input.Ref{}, err
	}
	digest, err := canonical.Hash(prepared.Params)
	if err != nil || ref.ArtifactID == uuid.Nil || ref.SHA256 != digest {
		return v2input.Ref{}, v2input.ErrConflict
	}
	if err := s.recheckSourcePreparationProof(ctx, who, tenant, binding, color, true, proof); err != nil {
		return v2input.Ref{}, err
	}
	return ref, nil
}
