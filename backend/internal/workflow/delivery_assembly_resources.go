package workflow

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// CompileDeliveryAssemblyProfile reserves JSON/index working memory, not raster
// decode buffers: assemble reads only bounded root/shard documents. The 16x JSON
// allowance and 1KiB per resolved ref are conservative planning estimates, not
// measured hardware capacity. V30's fixed manifest cap is independently applied
// by SQL admission. No image processing or GPU memory is involved.
func CompileDeliveryAssemblyProfile(ctx context.Context, plan delivery.PreparePlan, parent uuid.UUID, root v2input.Ref, rootRaw []byte, shards [][]byte, resolved []v2input.Entry, limits DeliveryDerivativeLimits) (json.RawMessage, error) {
	required, err := plan.AssemblyRequiredInputs(ctx, parent, root, rootRaw, shards)
	if err != nil {
		return nil, err
	}
	if len(resolved) > 1000000 {
		return nil, v2input.ErrInvalid
	}
	inventory := map[uuid.UUID]v2input.Entry{}
	for _, entry := range resolved {
		if entry.Ref.ArtifactID == uuid.Nil || domainDigestInvalid(entry.Ref.SHA256) || len(entry.Contract) < 1 || len(entry.Contract) > 120 {
			return nil, v2input.ErrInvalid
		}
		if previous, ok := inventory[entry.Ref.ArtifactID]; ok && previous != entry {
			return nil, v2input.ErrInvalid
		}
		inventory[entry.Ref.ArtifactID] = entry
	}
	for _, entry := range required {
		if inventory[entry.Ref.ArtifactID] != entry {
			return nil, v2input.ErrInvalid
		}
	}
	documents := int64(len(rootRaw))
	for _, raw := range shards {
		documents += int64(len(raw))
	}
	estimate := ResourceEstimate{RAMBytes: (128 << 20) + documents*16 + int64(len(resolved)+1)*1024, VRAMBytes: 0, ScratchBytes: documents + (8 << 20), OutputBytes: 8 << 20, PixelCount: 0}
	if limits.RAMBytes < estimate.RAMBytes || limits.ScratchBytes < estimate.ScratchBytes || limits.OutputBytes < estimate.OutputBytes {
		return nil, domain.NewError("DELIVERY_RESOURCE_INVALID", 422)
	}
	return canonical.Marshal(map[string]any{"backend": "cpu", "renderer_version": "v2.2", "resource_estimate": estimate})
}

// CompilePublishedDeliveryAssemblyTask derives both required roots and execution
// resources from the same exact documents; resolver authority remains external.
func CompilePublishedDeliveryAssemblyTask(ctx context.Context, lease delivery.ExpansionLease, plan delivery.PreparePlan, root, params v2input.Ref, rootRaw []byte, shards [][]byte, resolved []v2input.Entry, limits DeliveryDerivativeLimits) (FrozenDeliveryAssemblyTask, error) {
	profile, err := CompileDeliveryAssemblyProfile(ctx, plan, lease.Run, root, rootRaw, shards, resolved, limits)
	if err != nil {
		return FrozenDeliveryAssemblyTask{}, err
	}
	return FreezePublishedDeliveryAssemblyTask(ctx, lease, plan, root, params, rootRaw, shards, resolved, profile)
}

type DeliveryAssemblyPreparationReader interface {
	ReadAssemblyPreparation(context.Context, delivery.ExpansionLease, delivery.PreparePlan, v2input.Ref) (delivery.AssemblyPreparationEvidence, error)
	Renew(context.Context, delivery.ExpansionLease) error
}

// PrepareCurrentDeliveryAssemblyTask obtains exact documents and the complete
// inventory from current SQL authority, computes resources and freezes transport.
// No caller-supplied root/shard bytes or input list enters this preparation path.
func PrepareCurrentDeliveryAssemblyTask(ctx context.Context, store DeliveryAssemblyPreparationReader, lease delivery.ExpansionLease, plan delivery.PreparePlan, root, params v2input.Ref, limits DeliveryDerivativeLimits) (FrozenDeliveryAssemblyTask, error) {
	if err := ctx.Err(); err != nil {
		return FrozenDeliveryAssemblyTask{}, err
	}
	if store == nil {
		return FrozenDeliveryAssemblyTask{}, v2input.ErrInvalid
	}
	evidence, err := store.ReadAssemblyPreparation(ctx, lease, plan, root)
	if err != nil {
		return FrozenDeliveryAssemblyTask{}, err
	}
	frozen, err := CompilePublishedDeliveryAssemblyTask(ctx, lease, plan, root, params, evidence.RootJCS, evidence.ShardsJCS, evidence.Inventory, limits)
	if err != nil {
		return FrozenDeliveryAssemblyTask{}, err
	}
	if err := ctx.Err(); err != nil {
		return FrozenDeliveryAssemblyTask{}, err
	}
	if err := store.Renew(ctx, lease); err != nil {
		return FrozenDeliveryAssemblyTask{}, err
	}
	return frozen, nil
}


// Refuse incomplete inventory or impossible budgets before durable Params IO.
// SQL publication and registration still repeat current lease/source authority.
func prepareDeliveryAssemblyBeforePublication(ctx context.Context, store DeliveryAssemblyPreparationReader, lease delivery.ExpansionLease, plan delivery.PreparePlan, root v2input.Ref, limits DeliveryDerivativeLimits, publish func() (v2input.Ref, error)) (FrozenDeliveryAssemblyTask, error) {
 zero := FrozenDeliveryAssemblyTask{}
 if err := ctx.Err(); err != nil { return zero, err }
 if store == nil || publish == nil { return zero, v2input.ErrInvalid }
 evidence, err := store.ReadAssemblyPreparation(ctx, lease, plan, root)
 if err != nil { return zero, err }
 // Own the evidence across renewal and publication callbacks.
 evidence.RootJCS = append([]byte(nil), evidence.RootJCS...)
 shards := make([][]byte, len(evidence.ShardsJCS))
 for i, raw := range evidence.ShardsJCS { shards[i] = append([]byte(nil), raw...) }
 evidence.ShardsJCS = shards
 evidence.Inventory = append([]v2input.Entry(nil), evidence.Inventory...)
 profile, err := CompileDeliveryAssemblyProfile(ctx, plan, lease.Run, root, evidence.RootJCS, evidence.ShardsJCS, evidence.Inventory, limits)
 if err != nil { return zero, err }
 if err := ctx.Err(); err != nil { return zero, err }
 if err := store.Renew(ctx, lease); err != nil { return zero, err }
 if err := ctx.Err(); err != nil { return zero, err }
 params, err := publish()
 if err != nil { return zero, err }
 if err := ctx.Err(); err != nil { return zero, err }
 frozen, err := FreezePublishedDeliveryAssemblyTask(ctx, lease, plan, root, params, evidence.RootJCS, evidence.ShardsJCS, evidence.Inventory, profile)
 if err != nil { return zero, err }
 if err := store.Renew(ctx, lease); err != nil { return zero, err }
 if err := ctx.Err(); err != nil { return zero, err }
 return frozen, nil
}
