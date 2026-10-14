package workflow

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

type continuationAdmissionStore interface {
	Activated(context.Context) (bool, error)
	Registered(context.Context) (bool, error)
	Create(context.Context) error
	Commit(context.Context, *v2input.GraphTask) error
}

// AdmitContinuation performs blocked-task creation, transport publication and
// atomic registration/activation. Params and the physical prefix must already
// be pinned. Returns recovered=true only for an acknowledged prior activation.
func (f FrozenDeliveryContinuationTask) AdmitContinuation(ctx context.Context, db *pgxpool.Pool, publisher v2input.Publisher, lease delivery.ExpansionLease, plan delivery.PreparePlan, child delivery.ExportChildIntent) (bool, error) {
	if db == nil || lease.ParentTask != plan.TaskID() {
		return false, v2input.ErrInvalid
	}
	if _, _, err := plan.ExportRequest(lease.Run, child); err != nil {
		return false, err
	}
	store := sqlContinuationAdmission{db: db, frozen: f, lease: lease, plan: plan, child: child}
	return f.admitContinuation(ctx, store, publisher)
}
func (f FrozenDeliveryContinuationTask) admitContinuation(ctx context.Context, store continuationAdmissionStore, publisher v2input.Publisher) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if store == nil || publisher == nil {
		return false, v2input.ErrInvalid
	}
	if _, err := f.DraftTask(); err != nil {
		return false, err
	}
	activated, err := store.Activated(ctx)
	if err != nil || activated {
		return activated, err
	}
	registered, err := store.Registered(ctx)
	if err != nil {
		return false, err
	}
	if registered {
		return false, store.Commit(ctx, nil)
	}
	if err := store.Create(ctx); err != nil {
		return false, err
	}
	graph, err := f.PublishInputs(ctx, publisher)
	if err != nil {
		return false, err
	}
	return false, store.Commit(ctx, &graph)
}

type sqlContinuationAdmission struct {
	db     *pgxpool.Pool
	frozen FrozenDeliveryContinuationTask
	lease  delivery.ExpansionLease
	plan   delivery.PreparePlan
	child  delivery.ExportChildIntent
}

func (s sqlContinuationAdmission) activated(ctx context.Context, db delivery.ProofTransaction) (bool, error) {
	draft, err := s.frozen.DraftTask()
	if err != nil {
		return false, err
	}
	task, err := canonical.Marshal(draft)
	if err != nil {
		return false, err
	}
	var ready bool
	err = db.QueryRow(ctx, `SELECT photo.v30_delivery_continuation_activated($1,$2,$3,$4,$5,$6::jsonb,$7)`, s.lease.Expansion, s.lease.Token, s.child.SlotIndex, s.frozen.stage, s.frozen.predecessor, string(task), string(s.frozen.task.closure.ParamsJCS)).Scan(&ready)
	return ready, err
}
func (s sqlContinuationAdmission) Activated(ctx context.Context) (bool, error) {
	return s.activated(ctx, s.db)
}
func (s sqlContinuationAdmission) Registered(ctx context.Context) (bool, error) {
	return s.frozen.TransportRegistered(ctx, s.db, s.lease, s.plan, s.child)
}
func (s sqlContinuationAdmission) Create(ctx context.Context) error {
	return database.InTransaction(ctx, s.db, func(tx pgx.Tx) error {
		activated, err := s.activated(ctx, tx)
		if err != nil || activated {
			return err
		}
		registered, err := s.frozen.TransportRegistered(ctx, tx, s.lease, s.plan, s.child)
		if err != nil || registered {
			return err
		}
		return s.frozen.CreateInTransaction(ctx, tx, s.lease, s.plan, s.child)
	})
}
func (s sqlContinuationAdmission) Commit(ctx context.Context, graph *v2input.GraphTask) error {
	return database.InTransaction(ctx, s.db, func(tx pgx.Tx) error {
		activated, err := s.activated(ctx, tx)
		if err != nil || activated {
			return err
		}
		registered, err := s.frozen.TransportRegistered(ctx, tx, s.lease, s.plan, s.child)
		if err != nil {
			return err
		}
		if !registered {
			if graph == nil {
				return v2input.ErrInvalid
			}
			if err := s.frozen.RegisterInputsInTransaction(ctx, tx, s.lease, s.plan, s.child, *graph); err != nil {
				return err
			}
		}
		return s.frozen.ActivateInTransaction(ctx, tx, s.lease, s.plan, s.child)
	})
}
