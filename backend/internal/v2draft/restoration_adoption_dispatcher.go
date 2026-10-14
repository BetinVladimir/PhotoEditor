package v2draft

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

type RestorationAdoptionCandidates interface {
	Next(context.Context, uuid.UUID) (*RestorationAdoptionCandidate, error)
}

type RestorationCandidateAdopter interface {
	AdoptCandidate(context.Context, RestorationAdoptionCandidate) error
}

type RestorationAdoptionDispatcher struct {
	Candidates RestorationAdoptionCandidates
	Adopter    RestorationCandidateAdopter
	Timeout    time.Duration
	mu         sync.Mutex
	after      uuid.UUID
}

func (d *RestorationAdoptionDispatcher) ProcessOne(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if d == nil || d.Candidates == nil || d.Adopter == nil ||
		d.Timeout < time.Second || d.Timeout > 10*time.Minute {
		return false, v2input.ErrInvalid
	}
	if !d.mu.TryLock() {
		return false, nil
	}
	defer d.mu.Unlock()
	query, cancel := context.WithTimeout(ctx, 10*time.Second)
	candidate, err := d.Candidates.Next(query, d.after)
	cancel()
	if err != nil {
		return false, err
	}
	if candidate == nil {
		d.after = uuid.Nil
		return false, nil
	}
	if candidate.Scope.Tenant == uuid.Nil || candidate.Scope.Task == uuid.Nil ||
		candidate.Scope.Result == uuid.Nil || candidate.Run == uuid.Nil ||
		candidate.Actor.UserID == uuid.Nil || candidate.Actor.SessionID == uuid.Nil ||
		len(candidate.Descriptor) < 2 || len(candidate.Params) < 2 {
		return false, v2input.ErrInvalid
	}
	job := *candidate
	job.Descriptor = append([]byte(nil), candidate.Descriptor...)
	job.Params = append([]byte(nil), candidate.Params...)
	// A failed candidate is revisited after cursor wrap, without starving later
	// tasks in the same page of the durable SQL queue.
	d.after = job.Scope.Task
	work, stop := context.WithTimeout(ctx, d.Timeout)
	defer stop()
	if err = d.Adopter.AdoptCandidate(work, job); err != nil {
		return false, err
	}
	if err = work.Err(); err != nil {
		return false, err
	}
	return true, nil
}
