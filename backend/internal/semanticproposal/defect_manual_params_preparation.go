package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Keeps the exact compiled payload and original manual evidence together.
// This is only preparation: source/person/policy checks, publication ownership
// and final transactional admission remain required.
type defectManualParamsPreparation struct {
	params      PreparedDefectParams
	source      sourcePreparationProof
	manual      [64]defectManualExecutionProof
	manualCount int
}

// Only internal compiler results enter this constructor. Strings own every
// mutable buffer; publication reconstructs fresh dependency collections.
func freezeCompiledDefectParams(ctx context.Context, compiled review.DefectProposalTaskInputs) (PreparedDefectParams, error) {
	if err := ctx.Err(); err != nil {
		return PreparedDefectParams{}, err
	}
	if len(compiled.Params) < 2 || len(compiled.Params) > 1048576 || len(compiled.Inputs) < 2 || len(compiled.Inputs) > 4096 || compiled.ManualRoots == nil || len(compiled.ManualRoots) > 64 {
		return PreparedDefectParams{}, v2input.ErrInvalid
	}
	digest := sha256.Sum256(compiled.Params)
	if compiled.ParamsSHA256 != hex.EncodeToString(digest[:]) {
		return PreparedDefectParams{}, v2input.ErrInvalid
	}
	inputs, err := canonical.Marshal(compiled.Inputs)
	if err != nil {
		return PreparedDefectParams{}, err
	}
	roots, err := canonical.Marshal(compiled.ManualRoots)
	if err != nil {
		return PreparedDefectParams{}, err
	}
	if len(inputs) > 4194304 || len(roots) > 4194304 {
		return PreparedDefectParams{}, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return PreparedDefectParams{}, err
	}
	return PreparedDefectParams{params: string(compiled.Params), digest: compiled.ParamsSHA256, inputs: string(inputs), roots: string(roots)}, nil
}

func (s SQLTransferSourceReader) prepareManualDefectParams(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, faces []review.DefectProposalFaceInput, diameter [2]float64, permanent *review.ProtectionMask, additional []review.ProtectionMask) (defectManualParamsPreparation, error) {
	var zero defectManualParamsPreparation
	_, source, err := s.readQualifiedSourcePreparationProof(ctx, who, tenant, binding, color, true)
	if err != nil {
		return zero, err
	}
	compiled, proofs, err := s.prepareDefectManualTaskProof(ctx, who, tenant, binding, color, faces, diameter, permanent, additional)
	if err != nil {
		return zero, err
	}
	if len(proofs) > 64 {
		return zero, v2input.ErrInvalid
	}
	params, err := freezeCompiledDefectParams(ctx, compiled)
	if err != nil {
		return zero, err
	}
	result := defectManualParamsPreparation{params: params, source: source, manualCount: len(proofs)}
	for i, proof := range proofs {
		result.manual[i] = ownDefectManualExecutionProof(proof)
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if err := s.recheckSourcePreparationProof(ctx, who, tenant, binding, color, true, source); err != nil {
		return zero, err
	}
	return result, nil
}
