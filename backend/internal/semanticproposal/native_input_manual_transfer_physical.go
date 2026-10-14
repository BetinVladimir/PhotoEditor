package semanticproposal

import (
	"context"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Repeats full physical reference, target sources/person coverage and transfer
// qualification before SQL recovery/activation. No partial authority is returned.
func (s SQLTransferSourceReader) recheckNativeInputManualTransferPhysical(ctx context.Context, who auth.Identity, request Request, plan nativeInputManualTransferWorkflowPlan, configuration TransferPreparation) error {
	if err := s.validateNativeInputManualTransferPlanScope(ctx, who, request, plan, configuration); err != nil {
		return err
	}
	reference, err := s.readNativeInputManualTransferReferenceDocuments(ctx, who, request, plan.reference.person, configuration.Color, configuration.MetadataBytes)
	if err != nil {
		return err
	}
	if !sameNativeInputManualTransferDocuments(ctx, reference, plan.reference) {
		return v2input.ErrConflict
	}
	qualified, err := s.readTransferQualificationPreparation(ctx, who, request.TenantID, configuration.Qualification, configuration.Expected)
	if err != nil {
		return err
	}
	if qualified != *plan.core.transferQualification {
		return v2input.ErrConflict
	}
	if err := validateTransferMeasuredCosts(configuration.Costs, qualified.metrics); err != nil {
		return err
	}
	for i, target := range plan.core.inventory.Targets {
		_, source, err := s.readQualifiedSourcePreparationProof(ctx, who, request.TenantID, target, configuration.Color, false)
		if err != nil {
			return err
		}
		if source != plan.core.sources[2*i+1] {
			return v2input.ErrConflict
		}
		person := plan.core.person[i]
		current, err := s.selectPersonProtectionProof(ctx, who, request.TenantID, target, person.Protection, configuration.PersonProtection.Report, configuration.PersonProtection.Expected)
		if err != nil {
			return err
		}
		if current != person {
			return v2input.ErrConflict
		}
	}
	// Reference is checked last as well, after all later target object reads.
	after, err := s.readNativeInputManualTransferReferenceDocuments(ctx, who, request, plan.reference.person, configuration.Color, configuration.MetadataBytes)
	if err != nil {
		return err
	}
	if !sameNativeInputManualTransferDocuments(ctx, after, reference) {
		return v2input.ErrConflict
	}
	for i, target := range plan.core.inventory.Targets {
		if err := s.recheckSourcePreparationProof(ctx, who, request.TenantID, target, configuration.Color, false, plan.core.sources[2*i+1]); err != nil {
			return err
		}
		if err := s.recheckProtectionPreparationProof(ctx, who, request.TenantID, target, plan.core.person[i].protectionEvidence); err != nil {
			return err
		}
	}
	pins, err := s.qualificationObjects(ctx, who, request.TenantID, configuration.Qualification)
	if err != nil {
		return err
	}
	if len(pins) != 2 || pins[0] != qualified.objects[0] || pins[1] != qualified.objects[1] {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
