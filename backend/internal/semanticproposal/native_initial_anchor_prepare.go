package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// New-start boundary: publish the owner-authorized intent and current target
// snapshot before detector construction. No detector checkpoint is read and no
// producer/admission receipt is granted. Caller holds the original parent lock;
// existing detector checkpoints use restore rather than this preparation path.
func (b nativePreparationBootstrap) prepareInitialNativeAnchor(ctx context.Context, who auth.Identity, tenantID uuid.UUID, key string, raw []byte, capacity TransferPreparation, reserved int64) (nativePreparationAnchor, error) {
	var zero nativePreparationAnchor
	if err := b.validate(ctx); err != nil {
		return zero, err
	}
	if who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenantID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	capacity = ownTransferPreparation(capacity)
	if _, err := initialNativeFaceBudget(capacity, reserved, v2qualification.Metrics{}); err != nil {
		return zero, err
	}
	request, err := CompileRequest(tenantID, who.UserID, key, append([]byte(nil), raw...))
	if err != nil {
		return zero, err
	}
	published, err := PublishIntent(ctx, b.Documents, who, request)
	if err != nil {
		return zero, err
	}
	snapshot, body, err := b.Reader.PublishTargetSnapshot(ctx, b.Documents, who, request, key, published)
	if err != nil {
		return zero, err
	}
	var used int64
	if err := chargeMetadata(&used, capacity.MetadataBytes, len(request.Body), len(body)); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return nativePreparationAnchor{Request: request, Key: key, Published: published, Snapshot: snapshot, SnapshotBody: append([]byte(nil), body...), Color: capacity.Color}, nil
}
