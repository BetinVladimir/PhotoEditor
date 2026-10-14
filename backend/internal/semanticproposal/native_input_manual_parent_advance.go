package semanticproposal

import (
	"context"
	"github.com/google/uuid"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type nativeManualParentAdvance struct {
	phase  nativeInputPreparationPhase
	ticket v2input.JobTicket
}

// One bounded server-owned continuation step, without sleeping or in-memory
// checkpoints. Waiting/blocked/failed observations expose no parent ticket.
// The durable caller must persist/retry scheduling. Parent continuation probes
// original saved state and chooses complete recovery for an existing parent.
func (p TransferDraftPreparer) advanceNativeManualParent(ctx context.Context, catalog v2draft.ArtifactCatalog, who auth.Identity, anchor nativePreparationAnchor) (nativeManualParentAdvance, error) {
	var zero nativeManualParentAdvance
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if catalog == nil || p.Scheduler == nil || p.Reader.DB == nil || p.Reader.Objects == nil {
		return zero, v2input.ErrInvalid
	}
	if anchor.Key == "" {
		request, key, err := p.Reader.restoreNativeInputSavedRequest(ctx, who, anchor.Request, anchor.Published.Ref, anchor.Snapshot)
		if err != nil {
			return zero, err
		}
		anchor.Request, anchor.Key = request, key
	}
	request, err := CompileRequest(anchor.Request.TenantID, who.UserID, anchor.Key, anchor.Request.Body)
	if err != nil {
		return zero, err
	}
	if request.RunID != anchor.Request.RunID || request.WorkflowID != anchor.Request.WorkflowID || request.RequestSHA != anchor.Request.RequestSHA {
		return zero, v2input.ErrConflict
	}
	anchor.SnapshotBody = append([]byte(nil), anchor.SnapshotBody...)
	state, err := p.Reader.readNativeInputSavedStageState(ctx, who, request, anchor.Published.Ref, anchor.Snapshot, "defects.propose")
	if err != nil {
		return zero, err
	}
	phase, err := nativeInputStagePreparationPhase(state)
	if err != nil {
		return zero, err
	}
	if phase != nativePreparationSucceeded {
		return nativeManualParentAdvance{phase: phase}, nil
	}
	// Completion state is only a trigger. Independently restore physical producer,
	// qualification/manual evidence and committed child receipt before proceeding.
	pipeline, err := p.Reader.advanceSavedNativeFacePipeline(ctx, p.Scheduler, catalog, who, anchor)
	if err != nil {
		return zero, err
	}
	// A completed manual child cannot grant authority over pending or failed
	// upstream producers. Refuse contradictory state before parent publication.
	if pipeline.phase != nativePreparationSucceeded {
		return zero, v2input.ErrConflict
	}
	producers := pipeline.producers
	original, err := p.Reader.restoreNativeInputManualDraft(ctx, p.Scheduler, who, producers, anchor.Color)
	if err != nil {
		return zero, err
	}
	childTicket, err := p.Reader.resumeNativeInputManualPreparation(ctx, p.Scheduler, who, original, anchor.Color)
	if err != nil {
		return zero, err
	}
	built, err := p.Reader.recoverNativeInputManualTransport(ctx, p.Scheduler, who, original)
	if err != nil {
		return zero, err
	}
	plan, err := p.Reader.restoreNativeInputManualInitialPlan(ctx, p.Scheduler, who, producers, anchor.Color)
	if err != nil {
		return zero, err
	}
	ticket, err := p.prepareAndActivateManualParentFromSubmission(ctx, who, request.TenantID, anchor.Key, request.Body, nativeInputManualInitialSubmission{plan: plan, ticket: childTicket}, built)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return nativeManualParentAdvance{phase: nativePreparationSucceeded, ticket: ticket}, nil
}

// Restart entry point: only original tenant/parent IDs and authenticated server
// scope are required. The complete private anchor is read from saved objects.
func (p TransferDraftPreparer) advanceSavedNativeManualParent(ctx context.Context, catalog v2draft.ArtifactCatalog, who auth.Identity, tenantID, parent uuid.UUID) (nativeManualParentAdvance, error) {
	var zero nativeManualParentAdvance
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	p.Configuration = ownTransferPreparation(p.Configuration)
	if err := validateTransferPolicyConfiguration(p.Configuration); err != nil {
		return zero, err
	}
	if catalog == nil || p.Scheduler == nil {
		return zero, v2input.ErrInvalid
	}
	anchor, err := p.Reader.loadNativePreparationAnchor(ctx, who, tenantID, parent, p.Configuration.Color, p.Configuration.MetadataBytes)
	if err != nil {
		return zero, err
	}
	return p.advanceNativeManualParent(ctx, catalog, who, anchor)
}
