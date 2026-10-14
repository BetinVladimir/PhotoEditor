package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// VerifiedMultiSourceClosure is constructed only after source JSON and every
// physical array occurrence have passed current-scope and byte validation.
// It is evidence of a completed read, never a reusable permission or Apply proof.
// Output geometry, installed ICC and photographic quality are separate obligations.
type VerifiedMultiSourceClosure struct {
	originals VerifiedMultiOriginals
	verified  bool
}

func (v VerifiedMultiSourceClosure) Images() ([]review.WorkingImage, error) {
	if !v.verified {
		return nil, v2input.ErrInvalid
	}
	return v.originals.Images()
}
func (v VerifiedMultiSourceClosure) Documents() ([][]byte, error) {
	if !v.verified {
		return nil, v2input.ErrInvalid
	}
	return v.originals.Documents(), nil
}
func (v VerifiedMultiSourceClosure) Binding() (AdoptedMultiBinding, error) {
	if !v.verified {
		return AdoptedMultiBinding{}, v2input.ErrInvalid
	}
	return v.originals.binding, nil
}

// LoadAdoptedMultiSourceClosure never returns document-only proof as a complete
// source closure. All transactions finish before any object stream opens.
func LoadAdoptedMultiSourceClosure(ctx context.Context, source AdoptedMultiOriginalInventorySource, objects v2input.SessionAnalysisObjectReader, who auth.Identity, tenant, project, producer uuid.UUID, binding AdoptedMultiBinding, bucket string) (VerifiedMultiSourceClosure, error) {
	originals, err := LoadAdoptedMultiOriginals(ctx, source, objects, who, tenant, project, producer, binding, bucket)
	if err != nil {
		return VerifiedMultiSourceClosure{}, err
	}
	if err := VerifyAdoptedMultiOriginalArrays(ctx, source, objects, who, tenant, project, producer, originals); err != nil {
		return VerifiedMultiSourceClosure{}, err
	}
	if err := ctx.Err(); err != nil {
		return VerifiedMultiSourceClosure{}, err
	}
	return VerifiedMultiSourceClosure{originals: originals, verified: true}, nil
}
