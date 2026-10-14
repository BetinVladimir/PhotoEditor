package v2input

import (
	"bytes"
	"context"
	"encoding/json"
	"photoeditor/backend/internal/review"
)

// ResolveSemanticDefectLabels validates the complete hash-pinned proposal and
// binds every candidate identity to the exact accepted source document geometry.
// It does not qualify producer origin, decode NPY or select protected defects.
func ResolveSemanticDefectLabels(ctx context.Context, proposalRef Ref, proposalBody json.RawMessage, binding SessionVersionBinding, imageBody json.RawMessage) (Ref, error) {
	if err := ctx.Err(); err != nil {
		return Ref{}, err
	}
	if !validRef(proposalRef) || len(proposalBody) < 2 || len(proposalBody) > 1048576 {
		return Ref{}, ErrInvalid
	}
	if _, err := ResolveSemanticSourceMetadata(binding, imageBody); err != nil {
		return Ref{}, err
	}
	image, err := review.ParseWorkingImage(imageBody)
	if err != nil {
		return Ref{}, ErrInvalid
	}
	_, err = review.ReadDefectProposal(ctx, bytes.NewReader(proposalBody), review.ArtifactRef{ID: proposalRef.ArtifactID, SHA256: proposalRef.SHA256}, image.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256}))
	if err != nil {
		if ctx.Err() != nil {
			return Ref{}, ctx.Err()
		}
		return Ref{}, ErrInvalid
	}
	// Full strict proposal validation precedes this projection of its child ref.
	var projection struct {
		LabelMap review.ArtifactRef `json:"label_map"`
	}
	if json.Unmarshal(proposalBody, &projection) != nil {
		return Ref{}, ErrInvalid
	}
	return Ref{ArtifactID: projection.LabelMap.ID, SHA256: projection.LabelMap.SHA256}, nil
}
