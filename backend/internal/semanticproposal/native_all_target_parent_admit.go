package semanticproposal

import (
	"context"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Caller holds original parent advisory lock. Target readiness is independently
// reconstructed; only committed graph admission exposes a parent ticket.
func (p TransferDraftPreparer) admitPreparedNativeAllTargetParent(ctx context.Context, who auth.Identity, d detectorDraftPlan, advanced nativeInitialDefectAdvance, manifest nativeTargetPreparationManifest, catalogs NativeParentArtifactCatalogFactory) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if p.Scheduler == nil || p.Documents.DB == nil || p.Documents.Objects == nil || p.TransportDocuments == nil || catalogs == nil || advanced.phase != nativePreparationSucceeded {
		return zero, v2input.ErrInvalid
	}
	if err := verifyDetectorPlanSeal(ctx, d); err != nil {
		return zero, err
	}
	protection := advanced.protection
	defect := protection.published.preparation.plan
	if err := verifyNativeDefectDraftSeal(ctx, defect); err != nil {
		return zero, err
	}
	if defect.prepared.dependencies.preparation.landmarksPlan.detector.seal != d.seal {
		return zero, v2input.ErrConflict
	}
	existing, err := p.Reader.hasNativeAllTargetSavedParent(ctx, who, d.request)
	if err != nil {
		return zero, err
	}
	if existing {
		return p.loadAndResumeNativeAllTargetTransferLocked(ctx, who, d.request, d.requestKey, p.Configuration.Color)
	}
	sourcePerson := defect.prepared.dependencies.preparation.person
	if sourcePerson == nil {
		return zero, v2input.ErrConflict
	}
	c, err := p.Reader.prepareNativeTransferRuntimeConfiguration(ctx, who, d.request.TenantID, ownTransferPreparation(p.Configuration))
	if err != nil {
		return zero, err
	}
	selected, err := selectQualificationPolicies(sourcePerson.policy, []string{"mask.person"})
	if err != nil {
		return zero, err
	}
	c.PersonProtection = &TransferPersonProtection{Report: sourcePerson.report, Expected: selected["mask.person"]}
	if c.SourceProtection != (v2input.Ref{}) && c.SourceProtection != protection.verified.Root {
		return zero, v2input.ErrConflict
	}
	c.SourceProtection = protection.verified.Root
	_, source, err := p.Reader.readQualifiedSourcePreparationProof(ctx, who, d.request.TenantID, d.request.Intent.Reference, c.Color, false)
	if err != nil {
		return zero, err
	}
	person, err := p.Reader.prepareNativeInputReferencePersonMode(ctx, who, d.request, source, protection, c.Color, sourcePerson.report, true)
	if err != nil {
		return zero, err
	}
	reference, err := p.Reader.readNativeInputTransferReferenceDocuments(ctx, who, d.request, person, c.Color, c.MetadataBytes)
	if err != nil {
		return zero, err
	}
	p.Configuration = c
	targets, err := p.restoreNativeTransferTargets(ctx, who, d, manifest, reference, catalogs)
	if err != nil {
		return zero, err
	}
	c.TargetProtection = make([]v2input.Ref, len(targets))
	for i, target := range targets {
		c.TargetProtection[i] = target.protection.Root
	}
	inputs, documents, err := p.Reader.prepareNativeAllTargetTransferInputs(ctx, who, d.request, d.published, c, reference, targets)
	if err != nil {
		return zero, err
	}
	params, err := PublishTransferParams(ctx, p.Documents, who, d.request, d.requestKey, d.published, c.Qualification.ModelLockSHA, c.Qualification.Calibration)
	if err != nil {
		return zero, err
	}
	plan, err := p.Reader.compileNativeAllTargetTransferWorkflowPlan(ctx, who, d.request, d.published, d.anchor.snapshotRef, d.anchor.snapshotBody, params, inputs, documents, c)
	if err != nil {
		return zero, err
	}
	built, err := recoverNativeInputDefectTransport(ctx, p.Scheduler, who, defect)
	if err != nil {
		return zero, err
	}
	publisher := v2input.ScopedPublisher{Documents: p.TransportDocuments, Tenant: d.request.TenantID, Run: d.request.RunID}
	prepared, err := p.Reader.persistNativeAllTargetTransfer(ctx, p.Scheduler, publisher, who, d.request, d.requestKey, d.published, plan, c, built, p.ReservedBytes)
	if err != nil {
		return zero, err
	}
	transports := make([]v2input.Built, len(prepared))
	for i, task := range prepared {
		transports[i] = task.Graph.Built
	}
	return p.Reader.activateNativeAllTargetTransfer(ctx, p.Scheduler, who, d.request, d.requestKey, d.published, plan, c, built, transports)
}
