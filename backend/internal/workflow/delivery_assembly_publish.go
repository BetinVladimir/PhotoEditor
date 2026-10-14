package workflow

import (
	"context"
	"fmt"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
	"time"
)

// PublishDeliveryAssemblyInput writes and retains the exact complete matrix and its root.
// The returned ref still requires retained-closure admission before dispatch.
// Object IO occurs outside admission transactions; failed attempts can leave
// durable replayable documents, but never return a partly published root.
func PublishDeliveryAssemblyInput(ctx context.Context, db artifactjson.RowQuerier, objects artifactjson.Objects, bucket string, store delivery.MatrixObservationStore, lease delivery.ExpansionLease, plan delivery.PreparePlan) (v2input.Ref, string, error) {
	publishShard, err := NewDeliveryShardPublisher(db, objects, bucket, lease, plan)
	if err != nil {
		return v2input.Ref{}, "", err
	}
	shards := make([][]byte, 0, (lease.SlotCount+499)/500)
	capture := func(ctx context.Context, purpose string, body []byte) (v2input.Ref, error) {
		if purpose != fmt.Sprintf("v30_delivery_slots_%03d", len(shards)) {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		owned := append([]byte(nil), body...)
		ref, err := publishShard(ctx, purpose, owned)
		if err != nil {
			return v2input.Ref{}, err
		}
		shards = append(shards, owned)
		return ref, nil
	}
	physical, err := withDeliveryAssemblyLease(ctx, store, lease, 10*time.Second,
		func(work context.Context) (deliveryAssemblyIOResult, error) {
			_, body, _, state, err := plan.PrepareObservedAssembly(work, store, lease, capture)
			if err != nil {
				return deliveryAssemblyIOResult{}, err
			}
			if state != "assemble" {
				return deliveryAssemblyIOResult{state: state}, nil
			}
			publishRoot, err := NewDeliveryAssemblyPublisher(db, objects, bucket, lease, plan, shards)
			if err != nil {
				return deliveryAssemblyIOResult{}, err
			}
			if err := store.Renew(work, lease); err != nil {
				return deliveryAssemblyIOResult{}, err
			}
			ref, err := publishRoot(work, "v30_delivery_assembly", body)
			if err != nil {
				return deliveryAssemblyIOResult{}, err
			}
			return deliveryAssemblyIOResult{ref: ref, state: state}, nil
		})
	if err != nil {
		return v2input.Ref{}, "", err
	}
	if physical.state != "assemble" {
		return v2input.Ref{}, physical.state, nil
	}
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, "", err
	}
	if err := store.Renew(ctx, lease); err != nil {
		return v2input.Ref{}, "", err
	}
	if err := (delivery.ExpansionLeaseStore{DB: db}).RetainPublishedAssembly(ctx, lease, plan, physical.ref); err != nil {
		return v2input.Ref{}, "", err
	}
	return physical.ref, physical.state, nil
}
