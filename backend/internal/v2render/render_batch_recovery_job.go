package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"sync"
	"time"
)

// SQLRenderBatchRecovery serializes publication per batch across schedulers.
// A pinned session advisory lock spans object IO, without an open transaction.
// This does not activate a graph. Candidate selection must provide the persisted
// project and creator/session; the coordinator repeats current SQL authority.
type SQLRenderBatchRecovery struct {
	Factory SQLRenderBatchFactory
	Timeout time.Duration
	mu      sync.Mutex
	cursor  uuid.UUID
}

func (j *SQLRenderBatchRecovery) Recover(ctx context.Context, tenant, batch, project uuid.UUID, who auth.Identity) (done bool, err error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if j.Factory.Scheduler == nil || j.Factory.Scheduler.Config().MaxConns < 2 || j.Timeout <= 0 || j.Timeout > 30*time.Minute || tenant == uuid.Nil || batch == uuid.Nil || project == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return false, v2input.ErrInvalid
	}
	work, cancel := context.WithTimeout(ctx, j.Timeout)
	defer cancel()
	coordinator, err := j.Factory.ForProject(work, project)
	if err != nil {
		return false, err
	}
	conn, err := j.Factory.Scheduler.Acquire(work)
	if err != nil {
		return false, err
	}
	// Namespace collisions can only serialize unrelated work, never grant scope.
	key := "v26-render-recovery:" + tenant.String() + ":" + batch.String()
	var locked bool
	if err = conn.QueryRow(work, "SELECT pg_try_advisory_lock(hashtextextended($1,0))", key).Scan(&locked); err != nil {
		// An unknown lock outcome must never return this session to the pool.
		isolated := conn.Hijack()
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = isolated.Close(cleanup)
		return false, err
	}
	if !locked {
		conn.Release()
		return false, nil
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		var released bool
		cleanupErr := conn.QueryRow(cleanup, "SELECT pg_advisory_unlock(hashtextextended($1,0))", key).Scan(&released)
		if cleanupErr != nil || !released {
			if cleanupErr == nil {
				cleanupErr = errors.New("render recovery lock release failed")
			}
			isolated := conn.Hijack()
			_ = isolated.Close(cleanup)
			err = errors.Join(err, cleanupErr)
			done = false
		} else {
			conn.Release()
		}
	}()
	if err = coordinator.RecoverInputs(work, tenant, batch, who); err != nil {
		var scoped *domain.Error
		if errors.Is(err, v2input.ErrInvalid) || errors.Is(err, v2input.ErrExpired) || (errors.As(err, &scoped) && (scoped.HTTPStatus == 401 || scoped.HTTPStatus == 403 || scoped.HTTPStatus == 404)) {
			return false, renderRecoveryInputError{err}
		}
		return false, err
	}
	return true, nil
}

// ProcessOne rotates through candidate UUIDs, including after a failed recovery,
// so one rejected draft cannot monopolize this service loop. End-of-scan resets
// the in-memory cursor; persisted SQL eligibility controls retries after restart.
func (j *SQLRenderBatchRecovery) ProcessOne(ctx context.Context) (bool, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if j.Factory.Scheduler == nil || j.Timeout <= 0 || j.Timeout > 30*time.Minute {
		return false, v2input.ErrInvalid
	}
	read, stop := context.WithTimeout(ctx, j.Timeout)
	defer stop()
	var tenant, batch, project uuid.UUID
	var who auth.Identity
	var after any
	if j.cursor != uuid.Nil {
		after = j.cursor
	}
	err := j.Factory.Scheduler.QueryRow(read, "SELECT tenant_id,batch_id,project_id,actor_id,session_id,auth_epoch FROM photo.next_v26_render_recovery($1)", after).Scan(&tenant, &batch, &project, &who.UserID, &who.SessionID, &who.Epoch)
	if errors.Is(err, pgx.ErrNoRows) {
		j.cursor = uuid.Nil
		return false, nil
	}
	if err != nil {
		return false, err
	}
	j.cursor = batch
	done, recoveryErr := j.Recover(ctx, tenant, batch, project, who)
	if ctx.Err() != nil || (!done && recoveryErr == nil) {
		return done, recoveryErr
	}
	update, finish := context.WithTimeout(ctx, 5*time.Second)
	defer finish()
	var recorded bool
	updateErr := j.Factory.Scheduler.QueryRow(update, "SELECT photo.record_v26_render_recovery($1,$2,$3)", tenant, batch, recoveryErr != nil).Scan(&recorded)
	return renderRecoveryOutcome(done, recoveryErr, recorded, updateErr)
}

func renderRecoveryOutcome(done bool, recoveryErr error, recorded bool, updateErr error) (bool, error) {
	if updateErr != nil {
		return done, errors.Join(recoveryErr, updateErr)
	}
	_, rejected := recoveryErr.(renderRecoveryInputError)
	if recorded && rejected {
		return false, nil
	}
	return done, recoveryErr
}

// Only errors from the scoped coordinator are local input rejection. Factory,
// connection, storage and scheduling failures still affect loop readiness.
type renderRecoveryInputError struct{ cause error }

func (e renderRecoveryInputError) Error() string { return e.cause.Error() }
func (e renderRecoveryInputError) Unwrap() error { return e.cause }
