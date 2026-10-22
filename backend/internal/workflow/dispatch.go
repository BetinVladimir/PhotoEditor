package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/taskrecovery"
)

type ResourceEstimate = taskrecovery.ResourceEstimate

func ParseResourceEstimate(profile []byte) (ResourceEstimate, error) {
	return taskrecovery.ParseResourceEstimate(profile)
}

type Dispatcher struct {
	DB                  *pgxpool.Pool
	LegacyOnly          bool
	PreferModelAffinity bool
}

func (d Dispatcher) Dispatch(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 100 {
		return 0, errors.New("invalid dispatch limit")
	}
	count := 0
	for i := 0; i < limit; i++ {
		picked, queued, err := d.dispatchOne(ctx)
		if err != nil {
			return count, err
		}
		if queued {
			count++
		}
		if !picked {
			break
		}
	}
	return count, nil
}
func (d Dispatcher) dispatchOne(ctx context.Context) (picked, queued bool, err error) {
	err = database.InTransaction(ctx, d.DB, func(tx pgx.Tx) error {
		var e error
		picked, queued, e = d.dispatchInTransaction(ctx, tx)
		return e
	})
	return picked, queued, err
}
func (d Dispatcher) dispatchInTransaction(ctx context.Context, tx pgx.Tx) (picked, queued bool, err error) {
	return d.DispatchOneInTransaction(ctx, tx, nil)
}

// DispatchOneInTransaction preserves normal selection/admission/outbox behavior,
// while allowing a caller-owned transaction to reject a selected task before any
// node reservation, defer or fairness record. The caller MUST roll back on error.
// A nil validator retains the ordinary scheduler behavior. Validator receives a
// private profile copy, so observation cannot alter authoritative admission costs.
func (d Dispatcher) DispatchOneInTransaction(ctx context.Context, tx pgx.Tx, validate func(uuid.UUID, string, []byte) error) (picked, queued bool, err error) {
	if tx == nil {
		return false, false, errors.New("dispatch transaction required")
	}
	if err = ctx.Err(); err != nil {
		return false, false, err
	}
	var id uuid.UUID
	var class string
	var profile []byte
	query := "SELECT task_id,resource_class,profile FROM photo.pick_ready_task()"
	if d.LegacyOnly {
		query = "SELECT task_id,resource_class,profile FROM photo.pick_ready_legacy_task()"
	}
	err = tx.QueryRow(ctx, query).Scan(&id, &class, &profile)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	if validate != nil {
		if err = validate(id, class, append([]byte(nil), profile...)); err != nil {
			return true, false, err
		}
	}
	return d.dispatchSelectedInTransaction(ctx, tx, id, profile)
}

// Shared admission/outbox path for legacy selection and leader-owned V32 DRR.
func (d Dispatcher) dispatchSelectedInTransaction(ctx context.Context, tx pgx.Tx, id uuid.UUID, profile []byte) (picked, queued bool, err error) {
	estimate, estimateErr := ParseResourceEstimate(profile)
	reason := "waiting_worker"
	var nodes []string
	if estimateErr == nil {
		nodeQuery := "SELECT node_id FROM photo.eligible_dispatch_nodes($1)"
		if d.PreferModelAffinity {
			nodeQuery = "SELECT node_id FROM photo.capacity_affinity_dispatch_nodes($1)"
		}
		rows, e := tx.Query(ctx, nodeQuery, id)
		if e != nil {
			return true, false, e
		}
		for rows.Next() {
			var node string
			if e = rows.Scan(&node); e != nil {
				rows.Close()
				return true, false, e
			}
			nodes = append(nodes, node)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return true, false, e
		}
	} else {
		reason = "invalid_resource_estimate"
	}
	event, correlation := uuid.New(), uuid.New()
	for _, node := range nodes {
		// Rollback a failed candidate's savepoint releases its node/slot/domain locks
		// before trying another node. The fairness row remains held by the outer tx.
		candidate, e := tx.Begin(ctx)
		if e != nil {
			return true, false, e
		}
		var outcome string
		var command []byte
		e = candidate.QueryRow(ctx, "SELECT outcome,command FROM photo.reserve_dispatch($1,$2,$3,$4,$5,$6,$7,$8)", id, node, estimate.RAMBytes, estimate.VRAMBytes, estimate.ScratchBytes, event, correlation, "").Scan(&outcome, &command)
		if e != nil {
			candidate.Rollback(context.WithoutCancel(ctx))
			return true, false, e
		}
		if outcome == "queued" {
			if e = candidate.Commit(ctx); e != nil {
				return true, false, e
			}
			var recorded bool
			if e = tx.QueryRow(ctx, "SELECT photo.record_dispatch($1,$2)", id, event).Scan(&recorded); e != nil {
				return true, false, e
			}
			if !recorded {
				return true, false, errors.New("dispatch fairness record missing")
			}
			return true, true, nil
		}
		if e = candidate.Rollback(ctx); e != nil {
			return true, false, e
		}
		switch outcome {
		case "waiting_worker", "waiting_resource", "waiting_quota", "unsupported_backend", "unqualified_model", "input_unavailable":
			reason = outcome
		case "stale", "cancelled", "unsealed", "missing":
			return true, false, nil
		default:
			return true, false, errors.New("unknown resource admission outcome")
		}
	}
	var deferred bool
	if err = tx.QueryRow(ctx, "SELECT photo.defer_dispatch($1,$2)", id, reason).Scan(&deferred); err != nil {
		return true, false, err
	}
	return true, false, nil
}
