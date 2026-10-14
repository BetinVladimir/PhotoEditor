package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Deployment-selected original native producer context. These pins are private
// server authority; user intent cannot select its qualification/protection report.
type nativeTransferPreparation struct {
	Intent, Snapshot, Protection v2input.Ref
	Task                         uuid.UUID
	Reports                      nativeDefectQualificationReports
}

// Reconstructs original evidence after a process restart, prepares/replays the
// same frozen transfer graph, and returns only a committed acceptance ticket.
// Initial native producer bootstrap is separate; this requires its real result.
func (p TransferDraftPreparer) prepareAndActivateNativeTransfer(ctx context.Context, who auth.Identity, tenant uuid.UUID, key string, raw []byte, native nativeTransferPreparation) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	p.Configuration = ownTransferPreparation(p.Configuration)
	if p.Reader.DB == nil || p.Reader.Objects == nil || p.Documents.DB == nil || p.Documents.Objects == nil || p.ReservedBytes <= 0 || p.ReservedBytes > 9007199254740991 || native.Protection != p.Configuration.SourceProtection || p.Scheduler == nil || p.TransportDocuments == nil || p.Configuration.PersonProtection == nil || native.Task == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	if err := validateTransferConfiguration(p.Configuration); err != nil {
		return zero, err
	}
	request, err := CompileRequest(tenant, who.UserID, key, raw)
	if err != nil {
		return zero, err
	}
	_, proof, err := p.Reader.prepareNativeSourceContinuation(ctx, who, request, key, native.Intent, native.Snapshot, native.Task, native.Protection, native.Reports, *p.Configuration.PersonProtection, p.Configuration.Color)
	if err != nil {
		return zero, err
	}
	reference, err := p.Reader.readNativeTransferReferenceDocuments(ctx, who, request, proof, p.Configuration.Color, p.Configuration.MetadataBytes)
	if err != nil {
		return zero, err
	}
	prepared, err := p.prepareNativeDraft(ctx, who, tenant, key, raw, reference)
	if err != nil {
		return zero, err
	}
	return activateNativeTransfer(ctx, p.Scheduler, who, request, key, prepared.published, prepared)
}
