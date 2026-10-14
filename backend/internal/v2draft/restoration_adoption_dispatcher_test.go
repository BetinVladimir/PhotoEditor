package v2draft

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
)

type restorationCandidateQueueFixture struct{ candidate RestorationAdoptionCandidate }

func (q restorationCandidateQueueFixture) Next(_ context.Context, after uuid.UUID) (*RestorationAdoptionCandidate, error) {
	if after != uuid.Nil {
		return nil, nil
	}
	return &q.candidate, nil
}

type restorationCandidateAdopterFixture struct {
	calls int
	fail  bool
}

func (a *restorationCandidateAdopterFixture) AdoptCandidate(_ context.Context, _ RestorationAdoptionCandidate) error {
	a.calls++
	if a.fail {
		return errors.New("transient")
	}
	return nil
}

func TestRestorationAdoptionDispatcherRevisitsFailedCandidateAfterWrap(t *testing.T) {
	job := RestorationAdoptionCandidate{Scope: RestorationArrayScope{
		Tenant: uuid.New(), Task: uuid.New(), Result: uuid.New()},
		Run: uuid.New(), Actor: auth.Identity{UserID: uuid.New(), SessionID: uuid.New()},
		Descriptor: []byte("{}"), Params: []byte("{}")}
	adopter := &restorationCandidateAdopterFixture{fail: true}
	dispatcher := &RestorationAdoptionDispatcher{Candidates: restorationCandidateQueueFixture{job},
		Adopter: adopter, Timeout: time.Minute}
	if ok, err := dispatcher.ProcessOne(context.Background()); err == nil || ok || adopter.calls != 1 {
		t.Fatal("failed candidate reported successful")
	}
	if ok, err := dispatcher.ProcessOne(context.Background()); err != nil || ok || adopter.calls != 1 {
		t.Fatal("cursor did not wrap", err)
	}
	adopter.fail = false
	if ok, err := dispatcher.ProcessOne(context.Background()); err != nil || !ok || adopter.calls != 2 {
		t.Fatal("failed candidate not revisited", err)
	}
}
