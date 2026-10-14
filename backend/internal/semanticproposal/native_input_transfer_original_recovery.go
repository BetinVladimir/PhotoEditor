package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

func recoverNativeInputDefectTransport(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, plan nativeDefectDraftPlan) (v2input.Built, error) {
	var zero, saved v2input.Built
	if err := verifyNativeDefectDraftSeal(ctx, plan); err != nil {
		return zero, err
	}
	if scheduler == nil {
		return zero, v2input.ErrInvalid
	}
	err := database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
		built, err := recoverNativeDefectTransportInTransaction(ctx, tx, who, plan)
		if err != nil {
			return err
		}
		saved = built
		return nil
	})
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return saved, nil
}

// Original intent/snapshot and protected/manual input selection must be loaded
// by the server. Unlike the lower-level resume boundary, this reconstructs all
// private model/defect/protection/person proofs; callers supply none of them.
// No publishers or draft creation are used in this recovery path.
func (p TransferDraftPreparer) restoreAndResumeNativeInputTransfer(ctx context.Context, catalog v2draft.ArtifactCatalog, who auth.Identity, tenant uuid.UUID, key string, raw []byte, published PublishedIntent, snapshot v2input.Ref, snapshotBody []byte, diameter [2]float64, permanent *review.ProtectionMask, additional []review.ProtectionMask, manuals []review.OriginalManualProtection, pins []nativeDefectInputPin) (v2input.JobTicket, error) {
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
	defect, err := p.Reader.restoreNativeInputDefectDraftPlan(ctx, p.Scheduler, who, producers, p.Configuration.Color, diameter, permanent, additional, manuals, pins)
	if err != nil {
		return zero, err
	}
	built, err := recoverNativeInputDefectTransport(ctx, p.Scheduler, who, defect)
	if err != nil {
		return zero, err
	}
	registered, err := p.Reader.restoreRegisteredNativeInputProtection(ctx, who, defect, p.Configuration.Color)
	if err != nil {
		return zero, err
	}
	if registered.verified.Root != p.Configuration.SourceProtection {
		return zero, v2input.ErrConflict
	}
	_, transferSource, err := p.Reader.readQualifiedSourcePreparationProof(ctx, who, request.TenantID, request.Intent.Reference, p.Configuration.Color, false)
	if err != nil {
		return zero, err
	}
	person, err := p.Reader.prepareNativeInputReferencePerson(ctx, who, request, transferSource, registered, p.Configuration.Color, p.Configuration.PersonProtection.Report)
	if err != nil {
		return zero, err
	}
	// The existing resume repeats physical content and complete SQL authority
	// inside commit-aware admission recovery; this reconstruction is preparatory.
	return p.loadAndResumeNativeInputTransfer(ctx, who, tenant, key, raw, person, built)
}
