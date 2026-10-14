package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

// DeliveryChildStore commits DB-only stages. Activate must check the durable
// receipt again inside its transaction before attempting input registration.
// A nil receipt is unconfirmed, never permission to bypass SQL source fences.
type DeliveryChildStore interface {
	Receipt(context.Context, FrozenDeliveryInitialTask, delivery.ExpansionLease, delivery.PreparePlan, delivery.ExportChildIntent, uuid.UUID) (*DeliveryChildReceipt, error)
	Draft(context.Context, FrozenDeliveryInitialTask, delivery.ExpansionLease, delivery.PreparePlan, delivery.ExportChildIntent, uuid.UUID, int64) error
	Activate(context.Context, FrozenDeliveryInitialTask, delivery.ExpansionLease, delivery.PreparePlan, delivery.ExportChildIntent, uuid.UUID, v2input.GraphTask) error
}

type DeliveryChildAdmission struct {
	RunID      uuid.UUID
	ExportID   uuid.UUID
	NextCursor int
	Recovered  bool
}

// AdmitNewChild joins frozen compilation to draft/transport/activation. Params
// must already be ready and the closure must come from an authorized resolver.
// No object publication occurs within a store transaction. On an uncertain
// commit, retry with the same frozen input; the deterministic export ID and
// receipt prevent re-registering an already dispatched initial task.
func (f FrozenDeliveryInitialTask) AdmitNewChild(ctx context.Context, store DeliveryChildStore, publisher v2input.Publisher, lease delivery.ExpansionLease, plan delivery.PreparePlan, child delivery.ExportChildIntent) (DeliveryChildAdmission, error) {
	zero := DeliveryChildAdmission{}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if store == nil || publisher == nil || lease.ParentTask != plan.TaskID() || child.SlotIndex < 0 || child.SlotIndex > lease.Cursor || child.SlotIndex >= lease.SlotCount {
		return zero, v2input.ErrInvalid
	}
	draft, err := f.DraftTask()
	if err != nil {
		return zero, err
	}
	exportID, err := plan.NewChildExportID(lease.Run, child)
	if err != nil {
		return zero, err
	}
	config, _, err := f.childConfig(lease.Run, plan, child, exportID)
	if err != nil {
		return zero, err
	}
	estimate, err := ParseResourceEstimate(draft.ExecutionProfile)
	if err != nil {
		return zero, err
	}
	overhead := int64(len(config) + len(f.closure.ParamsJCS))
	const maxExactInteger int64 = 9007199254740991
	if estimate.OutputBytes < 1 || estimate.OutputBytes > maxExactInteger-overhead {
		return zero, v2input.ErrInvalid
	}
	receipt, err := store.Receipt(ctx, f, lease, plan, child, exportID)
	if err != nil {
		return zero, err
	}
	if receipt != nil {
		if receipt.RunID != child.RunID || receipt.ExportID != exportID || receipt.TaskID != draft.ID || receipt.Cursor <= child.SlotIndex || receipt.Cursor > lease.SlotCount {
			return zero, v2input.ErrInvalid
		}
		return DeliveryChildAdmission{RunID: child.RunID, ExportID: exportID, NextCursor: child.SlotIndex + 1, Recovered: true}, nil
	}
	if err := store.Draft(ctx, f, lease, plan, child, exportID, estimate.OutputBytes+overhead); err != nil {
		return zero, err
	}
	graph, err := f.PublishInputs(ctx, publisher)
	if err != nil {
		return zero, err
	}
	if err := store.Activate(ctx, f, lease, plan, child, exportID, graph); err != nil {
		return zero, err
	}
	return DeliveryChildAdmission{RunID: child.RunID, ExportID: exportID, NextCursor: child.SlotIndex + 1}, nil
}

// SQLDeliveryChildStore uses scheduler SQL functions. It does not own a lease,
// resolve source objects, implement cache reuse, or publish Params.
type SQLDeliveryChildStore struct{ DB *pgxpool.Pool }

func (s SQLDeliveryChildStore) Receipt(ctx context.Context, f FrozenDeliveryInitialTask, lease delivery.ExpansionLease, plan delivery.PreparePlan, child delivery.ExportChildIntent, id uuid.UUID) (*DeliveryChildReceipt, error) {
	if s.DB == nil {
		return nil, v2input.ErrInvalid
	}
	return f.RecoveryReceipt(ctx, s.DB, lease, plan, child, id)
}
func (s SQLDeliveryChildStore) Draft(ctx context.Context, f FrozenDeliveryInitialTask, lease delivery.ExpansionLease, plan delivery.PreparePlan, child delivery.ExportChildIntent, id uuid.UUID, reserved int64) error {
	if s.DB == nil {
		return v2input.ErrInvalid
	}
	return database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		// A concurrent/retried admission may have committed since the outer read.
		receipt, err := f.RecoveryReceipt(ctx, tx, lease, plan, child, id)
		if err != nil || receipt != nil {
			return err
		}
		return f.CreateDraft(ctx, tx, lease, plan, child, id, reserved)
	})
}
func (s SQLDeliveryChildStore) Activate(ctx context.Context, f FrozenDeliveryInitialTask, lease delivery.ExpansionLease, plan delivery.PreparePlan, child delivery.ExportChildIntent, id uuid.UUID, graph v2input.GraphTask) error {
	if s.DB == nil {
		return v2input.ErrInvalid
	}
	return database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		receipt, err := f.RecoveryReceipt(ctx, tx, lease, plan, child, id)
		if err != nil || receipt != nil {
			return err
		}
		return f.ActivateInTransaction(ctx, tx, lease, plan, child, id, graph)
	})
}
