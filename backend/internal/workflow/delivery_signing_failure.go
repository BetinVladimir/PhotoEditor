package workflow

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

type DeliverySignedFailureWriter interface {
	FailSignedPackage(context.Context, delivery.ExpansionLease,
		delivery.PreparePlan, delivery.Slot) error
}

type SQLDeliverySignedFailureStore struct {
	DB       *pgxpool.Pool
	Instance uuid.UUID
}

func (s SQLDeliverySignedFailureStore) FailExpiredPolicy(ctx context.Context,
	lease delivery.ExpansionLease, plan delivery.PreparePlan) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if s.DB == nil || s.Instance == uuid.Nil || !lease.Valid() ||
		lease.ParentTask != plan.TaskID() || lease.Cursor != lease.SlotCount {
		return false, v2input.ErrInvalid
	}
	var finalized bool
	err := assemblyTenantTransaction(ctx, s.DB, lease,
		func(call context.Context, tx pgx.Tx) error {
			return tx.QueryRow(call, `SELECT photo.fail_v31_delivery_expired_policy($1,$2,$3)`,
				lease.Expansion, lease.Token, s.Instance).Scan(&finalized)
		})
	return finalized, err
}

func (s SQLDeliverySignedFailureStore) FailSignedPackage(ctx context.Context,
	lease delivery.ExpansionLease, plan delivery.PreparePlan, slot delivery.Slot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil {
		return v2input.ErrInvalid
	}
	return assemblyTenantTransaction(ctx, s.DB, lease,
		func(call context.Context, tx pgx.Tx) error {
			return (delivery.ExpansionLeaseStore{DB: tx}).FailSignedPackage(
				call, lease, plan, slot)
		})
}
