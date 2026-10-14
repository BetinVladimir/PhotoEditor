package semanticproposal

import (
	"context"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func (s SQLTransferSourceReader) readNativeInputSavedDefectDiameter(ctx context.Context, who auth.Identity, request Request, intent, snapshot v2input.Ref) ([2]float64, error) {
	policy, err := s.readNativeInputSavedDefectPolicy(ctx, who, request, intent, snapshot)
	if err != nil {
		return [2]float64{}, err
	}
	return policy.Diameter, nil
}

// Source-bound original declarations, not independent protection/manual proofs.
// Every mask must still be physically read/qualified and locked before native
// admission; this reader does not relax validateNativeDefectInputAuthorityScope.
func (s SQLTransferSourceReader) readNativeInputSavedDefectPolicy(ctx context.Context, who auth.Identity, request Request, intent, snapshot v2input.Ref) (review.DefectProposalInputPolicy, error) {
	var zero review.DefectProposalInputPolicy
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	saved, raw, err := s.readNativeInputDefectSavedParams(ctx, who, request, intent, snapshot)
	if err != nil {
		return zero, err
	}
	body, err := s.Read(ctx, who, request.TenantID, request.Intent.Reference)
	if err != nil {
		return zero, err
	}
	image, err := review.ParseWorkingImage(body)
	if err != nil {
		return zero, err
	}
	ref := request.Intent.Reference.Source
	source := image.Source(review.ArtifactRef{ID: ref.ArtifactID, SHA256: ref.SHA256})
	policy, err := review.DefectProposalInputPolicyFromParams(review.ArtifactRef{ID: saved.params.ArtifactID, SHA256: saved.params.SHA256}, raw, source)
	if err != nil {
		return zero, err
	}
	after, current, err := s.readNativeInputDefectSavedParams(ctx, who, request, intent, snapshot)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(saved, after) || string(raw) != string(current) {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return policy, nil
}
