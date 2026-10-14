package semanticproposal

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// Rotate projects even after a rejected attempt. SQL eligibility/due times are
// authoritative after restart; the cursor only provides fairness within a scan.
type SQLNativeParentRecoveryDispatcher struct {
	Factory SQLNativeParentRecoveryFactory
	mu      sync.Mutex
	cursor  uuid.UUID
}

func (d *SQLNativeParentRecoveryDispatcher) ProcessOne(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if d == nil || d.Factory.Scheduler == nil {
		return false, v2input.ErrInvalid
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	var after any
	if d.cursor != uuid.Nil {
		after = d.cursor
	}
	var project *uuid.UUID
	if err := d.Factory.Scheduler.QueryRow(ctx, "SELECT photo.next_v27_native_parent_recovery_project($1)", after).Scan(&project); err != nil {
		return false, err
	}
	if project == nil {
		d.cursor = uuid.Nil
		return false, nil
	}
	if *project == uuid.Nil {
		return false, errors.New("native recovery project locator invalid")
	}
	d.cursor = *project
	job, err := d.Factory.ForProject(ctx, *project)
	if err != nil {
		return false, err
	}
	return job.ProcessOne(ctx)
}
