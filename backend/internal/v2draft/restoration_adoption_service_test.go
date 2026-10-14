package v2draft

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type restorationEvidenceVerifierFixture struct {
	candidate RestorationReviewCandidate
	evidence  RestorationVerificationEvidence
	err       error
}

func (f restorationEvidenceVerifierFixture) VerifyWithEvidence(context.Context, auth.Identity,
	RestorationArrayScope, string, []byte) (RestorationReviewCandidate, int64,
	RestorationVerificationEvidence, error) {
	return f.candidate, 1, f.evidence, f.err
}

type restorationAdoptionCommitFixture struct {
	calls int
	err   error
}

func (f *restorationAdoptionCommitFixture) Commit(_ context.Context,
	_ RestorationVerificationEvidence) error {
	f.calls++
	return f.err
}

func TestRestorationAdoptionReturnsOnlyCommittedCandidate(t *testing.T) {
	scope := RestorationArrayScope{Tenant: uuid.New(), Task: uuid.New(), Result: uuid.New()}
	candidate := RestorationReviewCandidate{Result: v2input.Ref{ArtifactID: uuid.New()}}
	store := &restorationAdoptionCommitFixture{}
	service := RestorationAdoptionService{Verifier: restorationEvidenceVerifierFixture{candidate: candidate}, Store: store}
	got, count, err := service.Adopt(context.Background(), auth.Identity{}, scope, "image.reflection", nil)
	if err != nil || got.Result != candidate.Result || count != 1 || store.calls != 1 {
		t.Fatal("committed candidate not returned", err)
	}
	store.err = errors.New("commit rejected")
	got, count, err = service.Adopt(context.Background(), auth.Identity{}, scope, "image.reflection", nil)
	if err == nil || got != (RestorationReviewCandidate{}) || count != 0 || store.calls != 2 {
		t.Fatal("uncommitted candidate returned")
	}
	store.err = nil
	service.Verifier = restorationEvidenceVerifierFixture{err: v2input.ErrInvalid}
	_, _, err = service.Adopt(context.Background(), auth.Identity{}, scope, "image.reflection", nil)
	if err == nil || store.calls != 2 {
		t.Fatal("invalid evidence reached commit")
	}
}
