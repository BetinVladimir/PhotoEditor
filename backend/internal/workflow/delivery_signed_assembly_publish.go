package workflow

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

// PublishSignedDeliveryAssemblyBundle binds the private required-mode compiler
// to the lease-fenced V30 document repositories. The caller must first retain
// and independently verify the complete V31 signing closure. This does not
// register or activate the assemble task.
func PublishSignedDeliveryAssemblyBundle(ctx context.Context, db artifactjson.RowQuerier,
	objects artifactjson.Objects, bucket string, lease delivery.ExpansionLease,
	plan delivery.PreparePlan, frozen delivery.FrozenInput, snapshotRaw []byte,
	slots []delivery.Slot, actor uuid.UUID, dependencies []delivery.SigningDependency,
	receipts []delivery.SignedDependencyReceipt,
	) (delivery.SignedAssemblyPublication, error) {
	if err := ctx.Err(); err != nil { return delivery.SignedAssemblyPublication{}, err }
	if db == nil || objects == nil || !lease.Valid() || lease.Cursor != lease.SlotCount ||
		lease.ParentTask != plan.TaskID() || len(slots) != lease.SlotCount || actor == uuid.Nil {
		return delivery.SignedAssemblyPublication{}, v2input.ErrInvalid
	}
	shards, err := NewDeliveryShardPublisher(db, objects, bucket, lease, plan)
	if err != nil { return delivery.SignedAssemblyPublication{}, err }
	signed, err := NewSignedDeliveryDocumentPublisher(db, objects, bucket, lease, plan)
	if err != nil { return delivery.SignedAssemblyPublication{}, err }
	rootFactory := func(bodies [][]byte)(delivery.PublishDocument,error){
		return NewDeliveryAssemblyPublisher(db, objects, bucket, lease, plan, bodies)
	}
	return delivery.PublishSignedAssemblyBundle(ctx, frozen, snapshotRaw, slots, lease.Tenant,
		actor, lease.Run, dependencies, receipts, shards, rootFactory, signed)
}
