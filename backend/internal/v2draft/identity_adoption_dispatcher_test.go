package v2draft

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

type identityCandidateQueueFixture struct {
	candidate *IdentityAdoptionCandidate
	cursors   []uuid.UUID
}

func (q *identityCandidateQueueFixture) Next(_ context.Context, after uuid.UUID) (*IdentityAdoptionCandidate, error) {
	q.cursors = append(q.cursors, after)
	if len(q.cursors) > 1 {
		return nil, nil
	}
	return q.candidate, nil
}

type identityCandidateAdopterFixture struct {
	calls int
	fail  error
}

func (a *identityCandidateAdopterFixture) AdoptIdentity(_ context.Context, _ IdentityAdoptionCandidate) error {
	a.calls++
	return a.fail
}

func TestIdentityAdoptionDispatcherRevisitsFailedCandidateAfterWrap(t *testing.T) {
	job := &IdentityAdoptionCandidate{Scope: IdentityResultScope{Tenant: uuid.New(), Task: uuid.New(), Result: uuid.New(),
		Root: v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
		Params: v2input.Ref{ArtifactID: uuid.New(), SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}, Attempt: 1, Fence: 1}
	queue := &identityCandidateQueueFixture{candidate: job}
	adopter := &identityCandidateAdopterFixture{fail: errors.New("temporary")}
	d := &IdentityAdoptionDispatcher{Candidates: queue, Adopter: adopter, Timeout: time.Minute}
	if ok, err := d.ProcessOne(context.Background()); ok || err == nil {
		t.Fatal("failed candidate accepted")
	}
	if ok, err := d.ProcessOne(context.Background()); ok || err != nil {
		t.Fatal("cursor wrap failed", err)
	}
	if len(queue.cursors) != 2 || queue.cursors[0] != uuid.Nil || queue.cursors[1] != job.Scope.Task {
		t.Fatal("candidate cursor not advanced and wrapped")
	}
}

func TestIdentityAdoptionDispatcherRejectsUntrustedCandidateBeforeObjectIO(t *testing.T) {
	job := &IdentityAdoptionCandidate{Scope: IdentityResultScope{Tenant: uuid.New(), Task: uuid.New(), Result: uuid.New(),
		Root: v2input.Ref{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
		Params: v2input.Ref{ArtifactID: uuid.New(), SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}, Attempt: 1, Fence: 1}
	queue := &identityCandidateQueueFixture{candidate: job}
	adopter := &identityCandidateAdopterFixture{}
	d := &IdentityAdoptionDispatcher{Candidates: queue, Adopter: adopter, Timeout: time.Minute}
	job.Scope.Root.SHA256 = "invalid"
	if ok, err := d.ProcessOne(context.Background()); ok || !errors.Is(err, v2input.ErrInvalid) || adopter.calls != 0 || d.after != uuid.Nil {
		t.Fatal("untrusted candidate reached adopter", err)
	}
	job.Scope.Root.SHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if ok, err := d.ProcessOne(context.Background()); ok || err != nil || adopter.calls != 0 {
		t.Fatal("invalid candidate advanced cursor", err)
	}
}
