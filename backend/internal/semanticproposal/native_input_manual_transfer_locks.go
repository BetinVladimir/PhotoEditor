package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Locks every prepared input in the caller transaction. This does not register,
// seal or activate a graph; snapshot/transport/quota admission remains separate.
func (s SQLTransferSourceReader) lockNativeInputManualTransferPreparedInputs(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, plan nativeInputManualTransferWorkflowPlan, configuration TransferPreparation, completed json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	if err := s.validateNativeInputManualTransferPlanScope(ctx, who, request, plan, configuration); err != nil {
		return err
	}
	if err := LockInventoryInTransaction(ctx, tx, who, request, plan.core.inventory); err != nil {
		return err
	}
	if err := s.lockNativeInputManualTransferReference(ctx, tx, who, request, plan.reference, configuration.Color, completed); err != nil {
		return err
	}
	if err := s.lockTransferQualificationPreparation(ctx, tx, who, request.TenantID, *plan.core.transferQualification, configuration.Expected); err != nil {
		return err
	}
	for i := range plan.core.inventory.Targets {
		source := plan.core.sources[2*i+1]
		if err := s.lockSourcePreparationProof(ctx, tx, who, request.TenantID, source.binding, source); err != nil {
			return err
		}
	}
	return lockPersonProtectionProofsInTransaction(ctx, tx, who, request.TenantID, s.ProjectID, plan.core.person)
}
