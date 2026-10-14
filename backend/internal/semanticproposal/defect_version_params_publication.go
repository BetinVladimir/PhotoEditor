package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Uses the accepted-version draft document lifecycle before a new task exists.
// The request compiler supplies a stable document ID. This does not admit tasks.
func (s SQLTransferSourceReader) publishManualDefectVersionParams(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, publisher v2draft.Publisher, document uuid.UUID, prepared defectManualParamsPreparation) (PublishedDefectParams, error) {
	empty := PublishedDefectParams{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if document == uuid.Nil {
		return empty, v2input.ErrInvalid
	}
	if err := s.recheckManualDefectParams(ctx, who, tenant, binding, color, prepared); err != nil {
		return empty, err
	}
	var inputs, roots []review.ArtifactRef
	if json.Unmarshal([]byte(prepared.params.inputs), &inputs) != nil || json.Unmarshal([]byte(prepared.params.roots), &roots) != nil {
		return empty, v2input.ErrInvalid
	}
	image, err := s.Read(ctx, who, tenant, binding)
	if err != nil {
		return empty, err
	}
	ref, err := publisher.PublishDefectProposalParams(ctx, who, tenant, document, binding, image, []byte(prepared.params.params), inputs)
	if err != nil {
		return empty, err
	}
	if ref.ArtifactID == uuid.Nil || ref.SHA256 != prepared.params.digest {
		return empty, v2input.ErrInvalid
	}
	for _, input := range inputs {
		if input.ID == ref.ArtifactID {
			return empty, v2input.ErrInvalid
		}
	}
	if err := s.recheckManualDefectParams(ctx, who, tenant, binding, color, prepared); err != nil {
		return empty, err
	}
	return PublishedDefectParams{Ref: ref, Inputs: inputs, ManualRoots: roots}, nil
}
