package semanticproposal

import (
	"context"
	"encoding/json"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Prepares a policy declaration over original qualified person pixels. This is
// not a new MaskRef artifact, a coverage certificate or permission to admit a
// defect task. Consumers must retain p and lock its original authority in SQL.
// No thresholding, dilation, probability change or object publication occurs.
func (s SQLTransferSourceReader) prepareNativePersonProtectionInput(ctx context.Context, who auth.Identity, request Request, p nativePersonInputAuthority, image json.RawMessage) (review.ProtectionMask, error) {
	var zero review.ProtectionMask
	if err := s.validateNativePersonInputAuthority(ctx, who, request, p); err != nil {
		return zero, err
	}
	mask, err := projectNativePersonProtectionInput(ctx, p, image)
	if err != nil {
		return zero, err
	}
	// Repeat physical evidence, current policy, candidate and immutable pins.
	// The retained proof is compared in full; selecting another candidate cannot
	// silently replace the original input during recovery or preparation.
	after, err := s.readNativePersonInputAuthorityCandidate(ctx, who, request, p.report, &personCandidate{root: p.root, pixels: p.pixels})
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(after, p) {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return mask, nil
}

// Pure projection only. Qualification and producer authority are established
// by the caller above, never by accepting an arbitrary caller-provided mask.
func projectNativePersonProtectionInput(ctx context.Context, p nativePersonInputAuthority, image json.RawMessage) (review.ProtectionMask, error) {
	var zero review.ProtectionMask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	mask, err := review.ParseMaskRef([]byte(p.body))
	if err != nil || mask.ModelLockID == nil {
		return zero, v2input.ErrInvalid
	}
	pixels, err := v2input.ResolveSemanticPersonMaskPixels(p.root, json.RawMessage(p.body), p.binding, image, *mask.ModelLockID)
	if err != nil {
		return zero, err
	}
	if pixels != p.pixels {
		return zero, v2input.ErrConflict
	}
	model := *mask.ModelLockID
	return review.ProtectionMask{Artifact: mask.Artifact, Canvas: mask.Canvas, TransformChain: append(json.RawMessage(nil), mask.TransformChain...), ObjectID: nil, Selector: mask.Selector, Confidence: mask.Confidence, ModelLockID: &model, Protected: true}, nil
}
