package v2draft

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type RestorationAdoptionCommitter interface {
	Commit(context.Context, RestorationVerificationEvidence) error
}

type RestorationEvidenceVerifier interface {
	VerifyWithEvidence(context.Context, auth.Identity, RestorationArrayScope, string, []byte) (
		RestorationReviewCandidate, int64, RestorationVerificationEvidence, error)
}

type RestorationAdoptionService struct {
	Verifier RestorationEvidenceVerifier
	Store    RestorationAdoptionCommitter
}

// Adopt returns a review candidate only after the atomic SQL receipt commits.
// Human review and Apply remain separate state transitions.
func (s RestorationAdoptionService) Adopt(ctx context.Context, who auth.Identity,
	scope RestorationArrayScope, operation string, paramsRaw []byte,
) (RestorationReviewCandidate, int64, error) {
	var zero RestorationReviewCandidate
	if s.Verifier == nil || s.Store == nil || scope.Tenant == uuid.Nil || scope.Task == uuid.Nil ||
		scope.Result == uuid.Nil {
		return zero, 0, v2input.ErrInvalid
	}
	candidate, count, evidence, err := s.Verifier.VerifyWithEvidence(ctx, who,
		scope, operation, paramsRaw)
	if err != nil {
		return zero, 0, err
	}
	if err = ctx.Err(); err != nil {
		return zero, 0, err
	}
	if err = s.Store.Commit(ctx, evidence); err != nil {
		return zero, 0, err
	}
	return candidate, count, nil
}
