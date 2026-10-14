package workflow

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

type DeliverySignedFailureWriter interface {
	FailSignedPackage(context.Context, delivery.ExpansionLease,
		delivery.PreparePlan, delivery.Slot) error
}

type SQLDeliverySignedFailureStore struct { DB *pgxpool.Pool }

func (s SQLDeliverySignedFailureStore) FailSignedPackage(ctx context.Context,
	lease delivery.ExpansionLease, plan delivery.PreparePlan, slot delivery.Slot) error {
	if err := ctx.Err(); err != nil { return err }
	if s.DB == nil { return v2input.ErrInvalid }
	return assemblyTenantTransaction(ctx, s.DB, lease,
		func(call context.Context, tx pgx.Tx) error {
			return (delivery.ExpansionLeaseStore{DB: tx}).FailSignedPackage(
				call, lease, plan, slot)
		})
}
