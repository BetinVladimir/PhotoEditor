package semanticproposal

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// SQL-only all-native barrier. Caller holds the original parent advisory lock
// and rolls back every error. Completed packets were physically checked before
// this transaction; SQL repeats current producers, policies and object pins.
func (s SQLTransferSourceReader) lockNativeAllTargetTransferPreparedInputs(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, plan nativeAllTargetTransferWorkflowPlan, c TransferPreparation, completed json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	if err := s.validateNativeAllTargetTransferPlanScope(ctx, who, request, plan, c); err != nil {
		return err
	}
	if err := LockInventoryInTransaction(ctx, tx, who, request, plan.core.inventory); err != nil {
		return err
	}
	if err := s.lockNativeAllTargetTransferReference(ctx, tx, who, request, plan.reference, c, completed); err != nil {
		return err
	}
	if plan.core.transferQualification == nil {
		return v2input.ErrInvalid
	}
	if err := s.lockTransferQualificationPreparation(ctx, tx, who, request.TenantID, *plan.core.transferQualification, c.Expected); err != nil {
		return err
	}
	for i, target := range plan.targets {
		if target.ordinal != i || target.binding != plan.core.inventory.Targets[i] {
			return v2input.ErrConflict
		}
		if err := s.lockSourcePreparationProof(ctx, tx, who, request.TenantID, target.binding, target.source); err != nil {
			return fmt.Errorf("native target %d source: %w", i, err)
		}
		if target.reuseReference {
			if target.completed != "" || target.plan != nil {
				return v2input.ErrInvalid
			}
			continue
		}
		if target.plan == nil || len(target.completed) < 2 || len(target.completed) > 32<<20 {
			return v2input.ErrInvalid
		}
		if err := s.lockNativeTargetDefectProtectionInTransaction(ctx, tx, who, *target.plan, target.registered, json.RawMessage(target.completed)); err != nil {
			return fmt.Errorf("native target %d protection: %w", i, err)
		}
	}
	return ctx.Err()
}

func (s SQLTransferSourceReader) lockNativeAllTargetTransferAdmissionEvidence(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, published PublishedIntent, plan nativeAllTargetTransferWorkflowPlan, c TransferPreparation, completed json.RawMessage) error {
	if err := s.lockNativeAllTargetTransferPreparedInputs(ctx, tx, who, request, plan, c, completed); err != nil {
		return err
	}
	if err := LockPublishedIntentInTransaction(ctx, tx, who, request, published); err != nil {
		return err
	}
	if err := LockSnapshotInTransaction(ctx, tx, who, request, plan.core); err != nil {
		return err
	}
	if err := LockQualificationInTransaction(ctx, tx, who, request, plan.core); err != nil {
		return err
	}
	return s.lockNativeAllTargetTransferPreparedInputs(ctx, tx, who, request, plan, c, completed)
}
