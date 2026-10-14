package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Rebuilds original manual source/protection/person authority from saved records.
// Caller supplies no masks, costs, limits or producer proofs. No publication or
// draft recreation is possible; parent resume uses only registered transport.
func (p TransferDraftPreparer) restoreAndResumeNativeInputManualTransfer(ctx context.Context, catalog v2draft.ArtifactCatalog, who auth.Identity, tenant uuid.UUID, key string, raw []byte, published PublishedIntent, snapshot v2input.Ref, snapshotBody []byte) (v2input.JobTicket, error) {
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
	producers, err := p.Reader.restoreNativeInputFaceProducers(ctx, catalog, who, request, key, published, snapshot, snapshotBody, p.Configuration.Color)
	if err != nil {
		return zero, err
	}
	original, err := p.Reader.restoreNativeInputManualDraft(ctx, p.Scheduler, who, producers, p.Configuration.Color)
	if err != nil {
		return zero, err
	}
	built, err := p.Reader.recoverNativeInputManualTransport(ctx, p.Scheduler, who, original)
	if err != nil {
		return zero, err
	}
	defect, err := p.Reader.restoreNativeInputManualInitialPlan(ctx, p.Scheduler, who, producers, p.Configuration.Color)
	if err != nil {
		return zero, err
	}
	registered, err := p.Reader.restoreRegisteredNativeInputManualInitialProtection(ctx, who, defect, p.Configuration.Color)
	if err != nil {
		return zero, err
	}
	if registered.verified.Root != p.Configuration.SourceProtection {
		return zero, v2input.ErrConflict
	}
	person, err := p.Reader.prepareNativeInputManualReferencePerson(ctx, who, request, producers.detector.prepared.source, registered, p.Configuration.Color, p.Configuration.PersonProtection.Report)
	if err != nil {
		return zero, err
	}
	return p.loadAndResumeNativeInputManualTransfer(ctx, who, tenant, key, raw, person, built)
}
