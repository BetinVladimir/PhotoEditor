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
type SQLNativeInitialRecoveryDispatcher struct {
	Factory SQLNativeParentRecoveryFactory
	mu      sync.Mutex
	cursor  uuid.UUID
}

func (d *SQLNativeInitialRecoveryDispatcher) ProcessOne(ctx context.Context) (bool, error) {
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
	if err := d.Factory.Scheduler.QueryRow(ctx, "SELECT photo.next_v27_native_initial_recovery_project($1)", after).Scan(&project); err != nil {
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
	job, err := d.Factory.InitialForProject(ctx, *project)
	if err != nil {
		return false, err
	}
	return job.ProcessOne(ctx)
}

// Rotate parent continuation, initial face pipeline and standalone person admission.
// Empty queues are skipped within the same bounded scan.
type SQLNativeRecoveryDispatcher struct {
	Parent    SQLNativeParentRecoveryDispatcher
	Initial   SQLNativeInitialRecoveryDispatcher
	mu        sync.Mutex
	nextQueue int
	person    SQLNativePersonAdmissionDispatcher
	ordinary  SQLOrdinaryParentDispatcher
}

func (d *SQLNativeRecoveryDispatcher) ProcessOne(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if d == nil {
		return false, v2input.ErrInvalid
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.person.Factory = d.Initial.Factory
	d.ordinary.Factory = d.Initial.Factory
	queues := []func(context.Context) (bool, error){d.Parent.ProcessOne, d.Initial.ProcessOne, d.person.ProcessOne, d.ordinary.ProcessOne}
	start := d.nextQueue
	d.nextQueue = (d.nextQueue + 1) % len(queues)
	for offset := 0; offset < len(queues); offset++ {
		worked, err := queues[(start+offset)%len(queues)](ctx)
		if worked || err != nil {
			return worked, err
		}
	}
	return false, nil
}
