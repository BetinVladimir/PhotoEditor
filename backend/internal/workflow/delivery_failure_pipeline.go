package workflow

import (
	"context"
 "github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

type DeliveryFailureWriter interface {
	FailPackage(context.Context, delivery.ExpansionLease, delivery.PreparePlan, delivery.FailureWitness) error
}

// FinalizeCurrentDeliveryFailure recovers an exact frozen witness from current
// observations. The writer must repeat SQL authority and commit atomically.
func FinalizeCurrentDeliveryFailure(ctx context.Context, reader delivery.MatrixObservationStore, writer DeliveryFailureWriter, lease delivery.ExpansionLease, plan delivery.PreparePlan) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if reader == nil || writer == nil {
		return false, v2input.ErrInvalid
	}
	observations, state, err := plan.ReadObservedMatrix(ctx, reader, lease)
	if err != nil {
		return false, err
	}
	if state != "failed" {
		return false, nil
	}
	witness, err := plan.FindFailureWitness(ctx, lease.Run, observations)
	if err != nil {
		return false, err
	}
	if witness == nil {
		return false, v2input.ErrInvalid
	}
	if err := reader.Renew(ctx, lease); err != nil {
		return false, err
	}
	if err := writer.FailPackage(ctx, lease, plan, *witness); err != nil {
		return false, err
	}
	return true, nil
}

func (s SQLDeliveryAssemblyStore) FailPackage(ctx context.Context, lease delivery.ExpansionLease, plan delivery.PreparePlan, witness delivery.FailureWitness) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil {
		return v2input.ErrInvalid
	}
 return assemblyTenantTransaction(ctx,s.DB,lease,func(ctx context.Context,tx pgx.Tx)error{
  return (delivery.ExpansionLeaseStore{DB:tx}).FailPackage(ctx,lease,plan,witness)
 })
}
