package semanticproposal

import (
	"context"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"reflect"
)

// Physical recheck outside SQL. No source-wide person rediscovery or generic
// imported protection is permitted in this continuation.
func (s SQLTransferSourceReader) recheckNativeAllTargetTransferPhysical(ctx context.Context, who auth.Identity, request Request, plan nativeAllTargetTransferWorkflowPlan, c TransferPreparation) error {
	if err := s.validateNativeAllTargetTransferPlanScope(ctx, who, request, plan, c); err != nil {
		return err
	}
	before, err := ReadInventory(ctx, s.DB, who, request)
	if err != nil {
		return err
	}
	if !sameTransferInventory(before, plan.core.inventory) {
		return v2input.ErrConflict
	}
	ref, err := s.readNativeInputTransferReferenceDocuments(ctx, who, request, plan.reference.person, c.Color, c.MetadataBytes)
	if err != nil {
		return err
	}
	if !sameNativeInputTransferDocuments(ctx, ref, plan.reference) {
		return v2input.ErrConflict
	}
	q, err := s.readTransferQualificationPreparation(ctx, who, request.TenantID, c.Qualification, c.Expected)
	if err != nil {
		return err
	}
	if q != *plan.core.transferQualification {
		return v2input.ErrConflict
	}
	if err := validateTransferMeasuredCosts(c.Costs, q.metrics); err != nil {
		return err
	}
	for _, target := range plan.targets {
		if err := s.recheckSourcePreparationProof(ctx, who, request.TenantID, target.binding, c.Color, target.source.unitRGB, target.source); err != nil {
			return err
		}
		if target.reuseReference {
			continue
		}
		if target.plan == nil {
			return v2input.ErrInvalid
		}
		current, err := s.readRegisteredNativeTargetDefectProtection(ctx, who, *target.plan, c.Color, target.registered.published)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current, target.registered) {
			return v2input.ErrConflict
		}
	}
	afterRef, err := s.readNativeInputTransferReferenceDocuments(ctx, who, request, plan.reference.person, c.Color, c.MetadataBytes)
	if err != nil {
		return err
	}
	if !sameNativeInputTransferDocuments(ctx, afterRef, ref) {
		return v2input.ErrConflict
	}
	for _, target := range plan.targets {
		if err := s.recheckSourcePreparationProof(ctx, who, request.TenantID, target.binding, c.Color, target.source.unitRGB, target.source); err != nil {
			return err
		}
		if target.plan == nil {
			continue
		}
		object, err := s.nativeTargetDefectProtectionObject(ctx, who, *target.plan)
		if err != nil {
			return err
		}
		result, err := s.nativeTargetDefectResultCatalog(ctx, who, *target.plan)
		if err != nil {
			return err
		}
		if object != target.registered.object || !reflect.DeepEqual(result, target.registered.published.preparation.completed.catalog) {
			return v2input.ErrConflict
		}
	}
	pins, err := s.qualificationObjects(ctx, who, request.TenantID, c.Qualification)
	if err != nil {
		return err
	}
	if len(pins) != 2 || pins[0] != q.objects[0] || pins[1] != q.objects[1] {
		return v2input.ErrConflict
	}
	after, err := ReadInventory(ctx, s.DB, who, request)
	if err != nil {
		return err
	}
	if !sameTransferInventory(before, after) {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
