package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Reconstructs the declared dependency inventory from original verified bytes,
// including optional source arrays and manual provenance roots. No publication
// occurs: Ref is the original canonical producer's Params. Descriptor selection,
// qualification and final locked admission remain separate requirements.
func (s SQLTransferSourceReader) prepareOriginDefectDependencies(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, pins DefectOriginPins) (PublishedDefectParams, defectManualParamsPreparation, error) {
	fail := func(err error) (PublishedDefectParams, defectManualParamsPreparation, error) {
		return PublishedDefectParams{}, defectManualParamsPreparation{}, err
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	origin, err := s.readDefectOriginPreparationProof(ctx, who, tenant, binding, pins)
	if err != nil {
		return fail(err)
	}
	prepared, err := s.prepareOriginDefectParams(ctx, who, tenant, binding, color, origin)
	if err != nil {
		return fail(err)
	}
	var inputs, roots []review.ArtifactRef
	if json.Unmarshal([]byte(prepared.params.inputs), &inputs) != nil || json.Unmarshal([]byte(prepared.params.roots), &roots) != nil {
		return fail(v2input.ErrInvalid)
	}
	published := PublishedDefectParams{Ref: origin.pins.Params, Inputs: inputs, ManualRoots: roots}
	if published.Ref.SHA256 != prepared.params.digest {
		return fail(v2input.ErrConflict)
	}
	if _, err := validateDefectPublishedRefs(published); err != nil {
		return fail(err)
	}
	if err := s.recheckManualDefectParams(ctx, who, tenant, binding, color, prepared); err != nil {
		return fail(err)
	}
	if err := s.recheckDefectOriginPreparationProof(ctx, who, tenant, binding, origin); err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return published, prepared, nil
}
