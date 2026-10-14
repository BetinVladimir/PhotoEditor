package workflow

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

// PublishDeliveryContinuationParams uses the same durable parent-scoped Params
// protocol as initial admission. SQL requires an open child continuation barrier
// and the exact predecessor physical-verification receipt. Call outside SQL TXs.
func PublishDeliveryContinuationParams(ctx context.Context, db *pgxpool.Pool, objects artifactjson.Objects, bucket string, lease delivery.ExpansionLease, plan delivery.PreparePlan, child delivery.ExportChildIntent, source []byte, results []DeliveryPresentationOutput, targetICC string, registry Registry, limits DeliveryDerivativeLimits) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if db == nil || objects == nil || len(results) < 1 || lease.ParentTask != plan.TaskID() {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	next, err := CompileDeliveryContinuation(plan, lease.Run, child, source, results, targetICC, registry, limits)
	if err != nil {
		return v2input.Ref{}, err
	}
	repository := deliveryParamsRepository{db: db, bucket: bucket, lease: lease, slot: child.SlotIndex, owner: next.Node.ID, params: append([]byte(nil), next.Node.Params...)}
	publisher := artifactjson.Publisher{Repository: repository, Objects: objects}
	ref, err := publisher.Publish(ctx, repository.scope(), repository.params)
	if err != nil {
		return v2input.Ref{}, err
	}
	return v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}, nil
}
