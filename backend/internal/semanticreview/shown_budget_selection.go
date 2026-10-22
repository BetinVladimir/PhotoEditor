package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
)

func NewShownBudgetSelection(config ExposureDeploymentBudget) (ExposureBudgetSelection, error) {
	const safe int64 = 9007199254740991
	pin := config.Qualification
	if config.Limits.VRAMBytes != 0 || !validExposureQualification(pin) || pin.ModelLockSHA != config.ModelLockSHA || pin.RuntimeLockSHA != config.RuntimeLockSHA || pin.DeviceProfileSHA != config.DeviceProfileSHA || pin.DatasetSHA != config.DatasetSHA {
		return nil, ErrInvalid
	}
	if _, err := CompileShownExposureAdoptionResources(1, 1, config.OwnerCosts, config.Limits); err != nil || config.Limits.PixelCount > 200000000 || config.OwnerCosts.RuntimeRAMBytes > safe {
		return nil, ErrInvalid
	}
	return func(ctx context.Context, who auth.Identity, tenant, intent uuid.UUID, request ApplyDecision) ([]ExposureInputBudget, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenant == uuid.Nil || intent == uuid.Nil {
			return nil, ErrInvalid
		}
		raw, err := canonical.Marshal(request)
		if err != nil {
			return nil, ErrInvalid
		}
		owned, err := DecodeApplyDecision(raw)
		if err != nil {
			return nil, err
		}
		result := make([]ExposureInputBudget, 0, len(owned.TargetDecisions))
		for _, decision := range owned.TargetDecisions {
			if decision.Decision == "approve" {
				result = append(result, ExposureInputBudget{TargetID: decision.TargetID, OwnerCosts: config.OwnerCosts, Limits: config.Limits, Qualification: pin})
			}
		}
		return result, nil
	}, nil
}
