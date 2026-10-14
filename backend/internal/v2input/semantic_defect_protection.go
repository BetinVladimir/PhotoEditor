package v2input

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

// ResolveSemanticDefectProtection binds the complete proposal and report to the
// exact source, then resolves the report's inline protected float32 mask child.
// Canonical producer/Params/profile qualification and pixel coverage are separate.
func ResolveSemanticDefectProtection(ctx context.Context, reportRef Ref, reportBody json.RawMessage, proposalRef Ref, proposalBody json.RawMessage, binding SessionVersionBinding, imageBody json.RawMessage) (Ref, error) {
	if err := ctx.Err(); err != nil {
		return Ref{}, err
	}
	if !validRef(reportRef) || reportRef.ArtifactID == proposalRef.ArtifactID || len(reportBody) < 2 || len(reportBody) > 1048576 {
		return Ref{}, ErrInvalid
	}
	digest := sha256.Sum256(reportBody)
	if hex.EncodeToString(digest[:]) != reportRef.SHA256 {
		return Ref{}, ErrInvalid
	}
	if _, err := ResolveSemanticDefectLabels(ctx, proposalRef, proposalBody, binding, imageBody); err != nil {
		return Ref{}, err
	}
	image, err := review.ParseWorkingImage(imageBody)
	if err != nil {
		return Ref{}, ErrInvalid
	}
	mask, err := review.ParseProposalProtection(reportBody, review.ArtifactRef{ID: proposalRef.ArtifactID, SHA256: proposalRef.SHA256}, image.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256}))
	if err != nil || mask.Selector != "manual" || mask.ObjectID != nil || mask.ModelLockID != nil {
		return Ref{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return Ref{}, err
	}
	return Ref{ArtifactID: mask.Artifact.ID, SHA256: mask.Artifact.SHA256}, nil
}

// BuildSemanticDefectProtectionDocument preserves the original report's complete
// inline MaskRef. No mask geometry or confidence is synthesized. The caller must
// independently verify canonical producer/pixels, publish immutable bytes and
// register source/result/parent provenance before any consumer can use the root.
func BuildSemanticDefectProtectionDocument(ctx context.Context, paramsRef Ref, paramsBody json.RawMessage, reportRef Ref, reportBody json.RawMessage, proposalRef Ref, proposalBody json.RawMessage, binding SessionVersionBinding, imageBody json.RawMessage) (json.RawMessage, Ref, error) {
	pixels, err := VerifySemanticDefectEvidence(ctx, paramsRef, paramsBody, reportRef, reportBody, proposalRef, proposalBody, binding, imageBody)
	if err != nil {
		return nil, Ref{}, err
	}
	image, err := review.ParseWorkingImage(imageBody)
	if err != nil {
		return nil, Ref{}, ErrInvalid
	}
	mask, err := review.ParseProposalProtection(reportBody, review.ArtifactRef{ID: proposalRef.ArtifactID, SHA256: proposalRef.SHA256}, image.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256}))
	if err != nil {
		return nil, Ref{}, ErrInvalid
	}
	body, err := canonical.Marshal(mask)
	if err != nil {
		return nil, Ref{}, err
	}
	if _, err := review.ParseMaskRef(body); err != nil {
		return nil, Ref{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, Ref{}, err
	}
	return json.RawMessage(body), pixels, nil
}
