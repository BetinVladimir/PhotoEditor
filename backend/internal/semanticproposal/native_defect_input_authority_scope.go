package semanticproposal

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"reflect"
)

// Temporary explicit scope boundary until independent original protection/manual
// authority is implemented. Called before publication and again before SQL writes.
func validateNativeDefectInputAuthorityScope(ctx context.Context, params PreparedDefectParams, additional []nativeDefectInputPin) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var protections struct {
		Permanent   *review.ProtectionMask  `json:"permanent"`
		Protections []review.ProtectionMask `json:"protections"`
	}
	var roots []review.ArtifactRef
	if json.Unmarshal([]byte(params.params), &protections) != nil || json.Unmarshal([]byte(params.roots), &roots) != nil {
		return v2input.ErrInvalid
	}
	if protections.Permanent != nil || len(protections.Protections) > 0 || len(roots) > 0 || len(additional) > 0 {
		return v2input.ErrInvalid
	}
	return nil
}

// Only blocked persistence/registration. Person authority has exact physical
// provenance and is locked with the native producers in the caller's SQL TX.
// This does not relax the separate composed admission/activation gate.
func validateNativeDefectBlockedInputAuthority(ctx context.Context, prepared nativeDefectFacePreparation, additional []nativeDefectInputPin) error {
	if prepared.person == nil {
		return validateNativeDefectInputAuthorityScope(ctx, prepared.params, additional)
	}
	if err := verifyNativeDefectPreparedParams(ctx, prepared); err != nil {
		return err
	}
	expected, err := nativeDefectPersonInputPins(ctx, prepared)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(expected, additional) {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
