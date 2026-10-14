package workflow

import (
	"context"

	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

// PublishSignedDeliveryAssemblyParams pins the published envelope as the one
// direct assembly Params root. The dedicated SQL read-back must succeed before
// the shared immutable task_params reserve/complete protocol is entered.
func PublishSignedDeliveryAssemblyParams(ctx context.Context, db artifactjson.RowQuerier,
	objects artifactjson.Objects, bucket string, lease delivery.ExpansionLease,
	plan delivery.PreparePlan, envelope v2input.Ref,
	) (v2input.Ref, []byte, error) {
	if lease.ParentTask != plan.TaskID() || lease.Cursor != lease.SlotCount {
		return v2input.Ref{}, nil, v2input.ErrInvalid
	}
	if _, _, err := ReadSignedDeliveryEnvelope(ctx, db, lease, envelope); err != nil {
		return v2input.Ref{}, nil, err
	}
	return PublishDeliveryAssemblyParams(ctx, db, objects, bucket, lease, plan, envelope)
}
