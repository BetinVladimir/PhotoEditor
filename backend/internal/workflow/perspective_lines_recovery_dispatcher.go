package workflow

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/v2input"
)

type PerspectiveLinesRecoveryCandidate struct{ Tenant, Run uuid.UUID }
type PerspectiveLinesRecoveryCandidates interface {
	NextPerspectiveLinesRecovery(context.Context, uuid.UUID) (*PerspectiveLinesRecoveryCandidate, error)
}
type PerspectiveLinesSealedResumer interface {
	ResumeSealed(context.Context, uuid.UUID, uuid.UUID) error
}
type SQLPerspectiveLinesRecoveryCandidates struct{ DB *pgxpool.Pool }

func (s SQLPerspectiveLinesRecoveryCandidates) NextPerspectiveLinesRecovery(ctx context.Context, after uuid.UUID) (*PerspectiveLinesRecoveryCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil {
		return nil, v2input.ErrInvalid
	}
	var cursor *uuid.UUID
	if after != uuid.Nil {
		cursor = &after
	}
	var c PerspectiveLinesRecoveryCandidate
	err := s.DB.QueryRow(ctx, "SELECT tenant_id,run_id FROM photo.next_v29_perspective_lines_recovery($1)", cursor).Scan(&c.Tenant, &c.Run)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if c.Tenant == uuid.Nil || c.Run == uuid.Nil {
		return nil, v2input.ErrInvalid
	}
	return &c, nil
}

// The cursor advances even after a failed candidate, preventing one stale draft
// from starving the remaining runs. Empty pages wrap on the next loop tick.
// Parallel scheduler instances may repeat IO; locked activation is idempotent.
type PerspectiveLinesRecoveryDispatcher struct {
	Candidates PerspectiveLinesRecoveryCandidates
	Resumer    PerspectiveLinesSealedResumer
	Timeout    time.Duration
	mu         sync.Mutex
	after      uuid.UUID
}

func (d *PerspectiveLinesRecoveryDispatcher) ProcessOne(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if d == nil {
		return false, v2input.ErrInvalid
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	budget := d.Timeout
	if budget == 0 {
		budget = 5 * time.Minute
	}
	if d.Candidates == nil || d.Resumer == nil || budget <= 0 || budget > 5*time.Minute {
		return false, v2input.ErrInvalid
	}
	query, done := context.WithTimeout(ctx, 5*time.Second)
	candidate, err := d.Candidates.NextPerspectiveLinesRecovery(query, d.after)
	done()
	if err != nil {
		return false, err
	}
	if candidate == nil {
		d.after = uuid.Nil
		return false, nil
	}
	if candidate.Tenant == uuid.Nil || candidate.Run == uuid.Nil || bytesUUIDCompare(candidate.Run, d.after) <= 0 {
		return false, v2input.ErrInvalid
	}
	d.after = candidate.Run
	work, stop := context.WithTimeout(ctx, budget)
	defer stop()
	if err := d.Resumer.ResumeSealed(work, candidate.Tenant, candidate.Run); err != nil {
		return false, err
	}
	return true, nil
}
func bytesUUIDCompare(a, b uuid.UUID) int {
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}
