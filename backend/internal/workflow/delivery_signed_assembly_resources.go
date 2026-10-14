package workflow

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// CompileSignedDeliveryAssemblyProfile accounts for the required-mode root,
// closure, envelope, every shard and the complete signed input inventory.
// Assemble indexes documents; it never decodes raster pixels on the GPU.
func CompileSignedDeliveryAssemblyProfile(ctx context.Context,
	publication delivery.SignedAssemblyPublication, lease delivery.ExpansionLease,
	actor uuid.UUID, slots []delivery.Slot,
	dependencies []delivery.SigningDependency,
	receipts []delivery.SignedDependencyReceipt,
	resolved []v2input.Entry, limits DeliveryDerivativeLimits,
) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil { return nil, err }
	if !lease.Valid() || lease.Cursor != lease.SlotCount ||
		actor == uuid.Nil || len(slots) != lease.SlotCount ||
		len(resolved) > 1000000 {
		return nil, v2input.ErrInvalid
	}
	required, err := delivery.SignedAssemblyRequiredRoots(
		publication.AssemblyRef, publication.Documents.AssemblyJCS,
		publication.ShardJCS, publication.ClosureRef,
		publication.Documents.ClosureJCS, publication.EnvelopeRef,
		lease.Tenant, actor, lease.Run, slots, dependencies, receipts)
	if err != nil { return nil, err }
	envelopeRaw, err := canonical.Marshal(publication.Envelope)
	if err != nil || !bytes.Equal(envelopeRaw, publication.EnvelopeJCS) ||
		renderDerivativeBytesSHA(envelopeRaw) != publication.EnvelopeRef.SHA256 {
		return nil, v2input.ErrInvalid
	}
	inventory := make(map[uuid.UUID]v2input.Entry, len(resolved))
	for _, entry := range resolved {
		if entry.Ref.ArtifactID == uuid.Nil ||
			domainDigestInvalid(entry.Ref.SHA256) ||
			len(entry.Contract) < 1 || len(entry.Contract) > 120 {
			return nil, v2input.ErrInvalid
		}
		if old, exists := inventory[entry.Ref.ArtifactID]; exists && old != entry {
			return nil, v2input.ErrInvalid
		}
		inventory[entry.Ref.ArtifactID] = entry
	}
	for _, entry := range required {
		if inventory[entry.Ref.ArtifactID] != entry {
			return nil, v2input.ErrInvalid
		}
	}
	documents := int64(len(publication.Documents.AssemblyJCS) +
		len(publication.Documents.ClosureJCS) + len(publication.EnvelopeJCS))
	for _, raw := range publication.ShardJCS { documents += int64(len(raw)) }
	estimate := ResourceEstimate{RAMBytes: (128 << 20) + documents*16 +
		int64(len(resolved)+1)*1024, VRAMBytes: 0,
		ScratchBytes: documents + (8 << 20),
		OutputBytes: 8 << 20, PixelCount: 0}
	if limits.RAMBytes < estimate.RAMBytes ||
		limits.ScratchBytes < estimate.ScratchBytes ||
		limits.OutputBytes < estimate.OutputBytes {
		return nil, domain.NewError("DELIVERY_RESOURCE_INVALID", 422)
	}
	return canonical.Marshal(map[string]any{"backend": "cpu",
		"renderer_version": "v2.2", "resource_estimate": estimate})
}
