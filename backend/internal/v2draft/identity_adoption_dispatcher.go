package v2draft

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
)

type IdentityAdoptionCandidate struct {
	Scope   IdentityResultScope
	Params  v2input.Ref
	Attempt int
	Fence   int64
}

type IdentityAdoptionCandidates interface {
	Next(context.Context, uuid.UUID) (*IdentityAdoptionCandidate, error)
}

type SQLIdentityAdoptionQueue struct {
	DB interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	}
}

func (q SQLIdentityAdoptionQueue) Next(ctx context.Context, after uuid.UUID) (*IdentityAdoptionCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if q.DB == nil {
		return nil, v2input.ErrInvalid
	}
	var cursor any
	if after != uuid.Nil {
		cursor = after
	}
	var item IdentityAdoptionCandidate
	err := q.DB.QueryRow(ctx, `SELECT tenant_id,task_id,result_id,root_id,root_sha,params_id,params_sha,attempt,fence
 FROM photo.next_v23_identity_adoption($1)`, cursor).Scan(&item.Scope.Tenant, &item.Scope.Task,
		&item.Scope.Result, &item.Scope.Root.ArtifactID, &item.Scope.Root.SHA256,
		&item.Params.ArtifactID, &item.Params.SHA256, &item.Attempt, &item.Fence)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if item.Scope.Tenant == uuid.Nil || item.Scope.Task == uuid.Nil ||
		item.Scope.Result == uuid.Nil || !isDocumentRef(item.Scope.Root) ||
		!isDocumentRef(item.Params) || item.Attempt < 1 || item.Fence < 1 ||
		(after != uuid.Nil && bytes.Compare(item.Scope.Task[:], after[:]) <= 0) {
		return nil, v2input.ErrInvalid
	}
	return &item, nil
}

type IdentityCandidateAdopter interface {
	AdoptIdentity(context.Context, IdentityAdoptionCandidate) error
}

type IdentityAdoptionDispatcher struct {
	Candidates IdentityAdoptionCandidates
	Adopter    IdentityCandidateAdopter
	Timeout    time.Duration
	mu         sync.Mutex
	after      uuid.UUID
}

func (d *IdentityAdoptionDispatcher) ProcessOne(ctx context.Context) (bool, error) {
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
	d.after = candidate.Scope.Task
	work, stop := context.WithTimeout(ctx, d.Timeout)
	defer stop()
	if err = d.Adopter.AdoptIdentity(work, *candidate); err != nil {
		return false, err
	}
	if err = work.Err(); err != nil {
		return false, err
	}
	return true, nil
}
