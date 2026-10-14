package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Publishes only the frozen native preparation; original dependencies must be
// live before and after publisher IO. The publisher remains responsible for
// SQL graph-owner authority and reserve/complete fencing. No task is enqueued.
func (s SQLTransferSourceReader) publishManualDefectParams(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, publisher v2input.DocumentPublisher, scope artifactjson.Scope, prepared defectManualParamsPreparation) (PublishedDefectParams, error) {
	empty := PublishedDefectParams{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if publisher == nil || tenant == uuid.Nil || scope.Tenant != tenant || scope.Run == uuid.Nil || scope.Owner == uuid.Nil || scope.Purpose != "task_params" {
		return empty, v2input.ErrInvalid
	}
	if err := s.recheckManualDefectParams(ctx, who, tenant, binding, color, prepared); err != nil {
		return empty, err
	}
	result, err := PublishPreparedDefectParams(ctx, publisher, scope, prepared.params)
	if err != nil {
		return empty, err
	}
	if err := s.recheckManualDefectParams(ctx, who, tenant, binding, color, prepared); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return result, nil
}
