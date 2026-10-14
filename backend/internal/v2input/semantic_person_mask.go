package v2input

import (
	"encoding/json"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
)

// ResolveSemanticPersonMaskPixels requires exact source geometry and the
// operator-pinned model identity. It does not prove model qualification,
// canonical producer origin or scientific completeness of person coverage.
func ResolveSemanticPersonMaskPixels(root Ref, body json.RawMessage, binding SessionVersionBinding, image json.RawMessage, modelLock string) (Ref, error) {
	if _, err := domain.ParseDigest(modelLock); err != nil {
		return Ref{}, ErrInvalid
	}
	pixels, err := ResolveSemanticMaskPixels(root, body, binding, image, false)
	if err != nil {
		return Ref{}, err
	}
	mask, err := review.ParseMaskRef(body)
	if err != nil || mask.Selector != "person" || mask.ObjectID != nil || mask.ModelLockID == nil || *mask.ModelLockID != modelLock {
		return Ref{}, ErrInvalid
	}
	return pixels, nil
}
