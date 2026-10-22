package v2draft

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type glareAuthorityFixture struct { calls int; deniedAt int; err error; scopes []RestorationArrayScope; cancelAt int; cancel context.CancelFunc }

func (f *glareAuthorityFixture) Current(_ context.Context, scope RestorationArrayScope) error {
	f.calls++
	f.scopes = append(f.scopes, scope)
	if f.calls == f.cancelAt && f.cancel != nil { f.cancel() }
	if f.calls == f.deniedAt { return f.err }
	return nil
}

type glarePhysicalFixture struct { calls int; err error; cancel context.CancelFunc }

func (f *glarePhysicalFixture) VerifyWithEvidence(context.Context, auth.Identity,
	RestorationArrayScope, string, []byte) (RestorationReviewCandidate, int64, RestorationVerificationEvidence, error) {
	f.calls++
	if f.cancel != nil { f.cancel() }
	return RestorationReviewCandidate{}, 12, RestorationVerificationEvidence{}, f.err
}

func TestGlareAdoptionRepeatsConsentAroundPhysicalIO(t *testing.T) {
	for _, tc := range []struct { name string; deniedAt int; physicalErr error; gateErr error; physicalCalls int; gateCalls int; committed int }{
		{"current", 0, nil, nil, 1, 2, 1},
		{"revoked_before_read", 1, nil, v2input.ErrExpired, 0, 1, 0},
		{"revoked_during_read", 2, nil, v2input.ErrExpired, 1, 2, 0},
		{"qualification_database_unavailable", 2, nil, errors.New("temporary database failure"), 1, 2, 0},
		{"physical_failure", 0, v2input.ErrInvalid, nil, 1, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope := RestorationArrayScope{Tenant: uuid.New(), Task: uuid.New(), Result: uuid.New()}
			gate := &glareAuthorityFixture{deniedAt: tc.deniedAt, err: tc.gateErr}
			physical := &glarePhysicalFixture{err: tc.physicalErr}
			store := &restorationAdoptionCommitFixture{}
			service := RestorationAdoptionService{Verifier: currentGlareAdoptionVerifier{Inner: physical, Authority: gate}, Store: store}
			_, _, err := service.Adopt(context.Background(), auth.Identity{}, scope, "portrait.glare", nil)
			if (err == nil) != (tc.committed == 1) || physical.calls != tc.physicalCalls || gate.calls != tc.gateCalls || store.calls != tc.committed {
				t.Fatalf("err=%v physical=%d gate=%d committed=%d", err, physical.calls, gate.calls, store.calls)
			}
			for _, got := range gate.scopes { if got != scope { t.Fatal("authority escaped frozen task/result scope") } }
		})
	}
}

func TestGlareAdoptionCancellationAndMissingAuthorityFailClosed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	gate := &glareAuthorityFixture{}
	physical := &glarePhysicalFixture{cancel: cancel}
	verifier := currentGlareAdoptionVerifier{Inner: physical, Authority: gate}
	_, _, _, err := verifier.VerifyWithEvidence(ctx, auth.Identity{}, RestorationArrayScope{}, "portrait.glare", nil)
	if !errors.Is(err, context.Canceled) || gate.calls != 1 { t.Fatal("cancelled physical read published evidence", err) }
	verifier.Authority = nil
	_, _, _, err = verifier.VerifyWithEvidence(context.Background(), auth.Identity{}, RestorationArrayScope{}, "portrait.glare", nil)
	if !errors.Is(err, v2input.ErrInvalid) { t.Fatal("missing live consent authority accepted") }
}

func TestGlareAdoptionLateAuthorityCallbackCannotPublish(t *testing.T) {
	for _, cancelAt := range []int{1, 2} {
		ctx, cancel := context.WithCancel(context.Background())
		gate := &glareAuthorityFixture{cancelAt: cancelAt, cancel: cancel}
		physical := &glarePhysicalFixture{}
		store := &restorationAdoptionCommitFixture{}
		service := RestorationAdoptionService{Verifier: currentGlareAdoptionVerifier{Inner: physical, Authority: gate}, Store: store}
		scope := RestorationArrayScope{Tenant: uuid.New(), Task: uuid.New(), Result: uuid.New()}
		_, _, err := service.Adopt(ctx, auth.Identity{}, scope, "portrait.glare", nil)
		if !errors.Is(err, context.Canceled) || store.calls != 0 || physical.calls != cancelAt-1 {
			t.Fatalf("late authority callback escaped cancellation: phase=%d err=%v reads=%d commits=%d", cancelAt, err, physical.calls, store.calls)
		}
		cancel()
	}
}
