package semanticproposal

import (
	"context"
	"errors"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// selectOrdinaryTransferConfiguration selects protection independently of the
// caller's reference mask. Only unique canonical producers for the original
// source are eligible; physical/person evidence is checked before compilation.
// Final admission still locks all prepared proofs and the exact inventory.
func (s SQLTransferSourceReader) selectOrdinaryTransferConfiguration(ctx context.Context, who auth.Identity, request Request, published PublishedIntent, base TransferPreparation) (TransferPreparation, error) {
	var zero TransferPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	base = ownTransferPreparation(base)
	if err := validateTransferPolicyConfiguration(base); err != nil {
		return zero, err
	}
	if base.PersonProtection == nil || base.SourceProtection != (v2input.Ref{}) || len(base.TargetProtection) != 0 || published.Inventory.Scope.ProjectID != s.ProjectID {
		return zero, v2input.ErrInvalid
	}
	candidate, err := s.transferTargetProtectionCandidate(ctx, who, request.TenantID, request.Intent.Reference)
	if err != nil {
		return zero, err
	}
	verified, err := s.ReadProtection(ctx, who, request.TenantID, request.Intent.Reference, candidate.root)
	if err != nil {
		return zero, err
	}
	if verified.Pixels != candidate.pixels {
		return zero, v2input.ErrConflict
	}
	proof, err := s.selectPersonProtectionProof(ctx, who, request.TenantID, request.Intent.Reference, candidate.root, base.PersonProtection.Report, base.PersonProtection.Expected)
	if err != nil {
		if errors.Is(err, errPersonProducerUnavailable) {
			return zero, errCreateReferencePersonUnavailable
		}
		return zero, err
	}
	var used int64
	if err := chargeMetadata(&used, base.MetadataBytes, len(verified.Body), len(proof.Evidence), len(proof.Qualification), len(proof.Expected)); err != nil {
		return zero, err
	}
	base.SourceProtection = verified.Root
	// Give target selection only the remaining retained metadata allowance.
	totalBudget := base.MetadataBytes
	base.MetadataBytes -= used
	selected, err := s.selectTransferTargetProtectionConfiguration(ctx, who, request, published, base)
	if err != nil {
		return zero, err
	}
	selected.MetadataBytes = totalBudget
	current, err := s.transferTargetProtectionCandidate(ctx, who, request.TenantID, request.Intent.Reference)
	if err != nil {
		return zero, err
	}
	if current != candidate {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return selected, nil
}
