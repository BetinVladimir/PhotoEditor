package semanticproposal

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/v2input"
)

// Shared serializer only after distinct complete target plan validation.
func marshalNativeTargetDefectRegistrationBinding(ctx context.Context, plan nativeTargetDefectDraftPlan, built v2input.Built) (json.RawMessage, error) {
	if err := verifyNativeTargetDefectPlanSeal(ctx, plan); err != nil {
		return nil, err
	}
	return marshalNativeDefectTaskRegistrationBinding(ctx, plan.prepared.frozen, plan.shell, built)
}
