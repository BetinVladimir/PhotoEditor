package semanticproposal

import (
	"context"
	"encoding/json"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Revalidates distinct original manual authority and binds exact original task
// transport. This packet is data for registration/admission, not a queue permit.
func (s SQLTransferSourceReader) marshalNativeInputManualRegistrationBinding(ctx context.Context, who auth.Identity, plan nativeInputManualDraftPlan, built v2input.Built) (json.RawMessage, error) {
	if err := verifyNativeInputManualDraftSeal(ctx, plan); err != nil {
		return nil, err
	}
	current, err := s.compileNativeInputManualDraft(ctx, who, plan.prepared.stage)
	if err != nil {
		return nil, err
	}
	if current.seal != plan.seal || !reflect.DeepEqual(current.shell, plan.shell) {
		return nil, v2input.ErrConflict
	}
	return marshalNativeDefectTaskRegistrationBinding(ctx, current.prepared.frozen, current.shell, built)
}
