package semanticproposal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"reflect"

	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Revalidates the full Params schema and geometry against the retained original
// WorkingImage. This binds declarations only; manual provenance and transaction
// locks remain separate requirements and this does not authorize admission.
func verifyNativeInputManualSelectionParams(ctx context.Context, selection nativeInputManualSelection) error {
	if err := verifyNativeInputManualParamsReplay(ctx, selection.params, selection.paramsRef, []byte(selection.params.params)); err != nil {
		return err
	}
	if len(selection.sourceBody) < 2 || len(selection.sourceBody) > 1<<20 {
		return v2input.ErrInvalid
	}
	sum := sha256.Sum256([]byte(selection.sourceBody))
	if hex.EncodeToString(sum[:]) != selection.binding.Source.SHA256 {
		return v2input.ErrConflict
	}
	image, err := review.ParseWorkingImage([]byte(selection.sourceBody))
	if err != nil {
		return err
	}
	ref := selection.binding.Source
	source := image.Source(review.ArtifactRef{ID: ref.ArtifactID, SHA256: ref.SHA256})
	policy, err := review.DefectProposalInputPolicyFromParams(review.ArtifactRef{ID: selection.paramsRef.ArtifactID, SHA256: selection.paramsRef.SHA256}, []byte(selection.params.params), source)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(policy, selection.policy) {
		return v2input.ErrConflict
	}
	if len(selection.manuals) > 64 {
		return v2input.ErrInvalid
	}
	var used int
	for _, manual := range selection.manuals {
		if len(manual.Body) > (4<<20)-used {
			return v2input.ErrInvalid
		}
		used += len(manual.Body)
	}
	// Recompile the complete closure, not just the Params payload: inputs and
	// roots are independent retained strings and must not lose or gain artifacts.
	compiled, err := review.BuildDefectProposalTaskInputsFromImage(ctx, source.Image, []byte(selection.sourceBody), policy.Faces, policy.Diameter, policy.Permanent, policy.Protections, selection.manuals)
	if err != nil {
		return err
	}
	expected, err := freezeCompiledDefectParams(ctx, compiled)
	if err != nil {
		return err
	}
	if expected != selection.params {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
