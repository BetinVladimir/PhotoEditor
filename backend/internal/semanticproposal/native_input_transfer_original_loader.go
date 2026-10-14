package semanticproposal

import (
	"context"
	"reflect"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Loads original intent/inventory and saved transfer snapshot internally before
// reconstructing private native authority. Protected/manual input selection is
// still a server duty; this does not authorize caller-supplied mask metadata.
func (p TransferDraftPreparer) loadOriginalAndResumeNativeInputTransfer(ctx context.Context, catalog v2draft.ArtifactCatalog, who auth.Identity, tenant uuid.UUID, key string, raw []byte, permanent *review.ProtectionMask, additional []review.ProtectionMask, manuals []review.OriginalManualProtection, pins []nativeDefectInputPin) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	p.Configuration = ownTransferPreparation(p.Configuration)
	if catalog == nil || p.Scheduler == nil || p.Reader.DB == nil || p.Reader.Objects == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || p.Configuration.PersonProtection == nil {
		return zero, v2input.ErrInvalid
	}
	if err := validateTransferConfiguration(p.Configuration); err != nil {
		return zero, err
	}
	request, err := CompileRequest(tenant, who.UserID, key, raw)
	if err != nil {
		return zero, err
	}
	published, err := p.Reader.readNativeInputSavedPublishedIntent(ctx, who, request, p.Configuration.MetadataBytes)
	if err != nil {
		return zero, err
	}
	saved, err := p.Reader.readNativeInputTransferSavedConfig(ctx, who, request, p.Configuration)
	if err != nil {
		return zero, err
	}
	p.Configuration.legacyNativeQueueOmitted = saved.legacyQueueOmitted
	snapshotBody, err := p.Reader.readNativeInputTransferSavedSnapshot(ctx, who, request, saved, p.Configuration.MetadataBytes)
	if err != nil {
		return zero, err
	}
	after, err := p.Reader.readNativeInputTransferSavedConfig(ctx, who, request, p.Configuration)
	if err != nil {
		return zero, err
	}
	if !reflect.DeepEqual(saved, after) {
		return zero, v2input.ErrConflict
	}
	diameter, err := p.Reader.readNativeInputSavedDefectDiameter(ctx, who, request, published.Ref, saved.snapshot)
	if err != nil {
		return zero, err
	}
	return p.restoreAndResumeNativeInputTransfer(ctx, catalog, who, tenant, key, raw, published, saved.snapshot, snapshotBody, diameter, permanent, additional, manuals, pins)
}
