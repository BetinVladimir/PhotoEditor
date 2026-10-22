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
		return zero, nativeAllTargetAdmissionError("context", err)
	}
	if p.Scheduler == nil || p.Documents.DB == nil || p.Documents.Objects == nil || p.TransportDocuments == nil || catalogs == nil || advanced.phase != nativePreparationSucceeded {
		return zero, v2input.ErrInvalid
	}
	if err := verifyDetectorPlanSeal(ctx, d); err != nil {
		return zero, nativeAllTargetAdmissionError("detector_seal", err)
	}
	protection := advanced.protection
	defect := protection.published.preparation.plan
	if err := verifyNativeDefectDraftSeal(ctx, defect); err != nil {
		return zero, nativeAllTargetAdmissionError("defect_seal", err)
	}
	if defect.prepared.dependencies.preparation.landmarksPlan.detector.seal != d.seal {
		return zero, nativeAllTargetAdmissionError("detector_link", v2input.ErrConflict)
	}
	existing, err := p.Reader.hasNativeAllTargetSavedParent(ctx, who, d.request)
	if err != nil {
		return zero, nativeAllTargetAdmissionError("saved_parent_lookup", err)
	}
	if existing {
		ticket, err := p.loadAndResumeNativeAllTargetTransferLocked(ctx, who, d.request, d.requestKey, p.Configuration.Color)
		return ticket, nativeAllTargetAdmissionError("saved_parent_replay", err)
	}
	sourcePerson := defect.prepared.dependencies.preparation.person
	if sourcePerson == nil {
		return zero, nativeAllTargetAdmissionError("source_person", v2input.ErrConflict)
	}
	c, err := p.Reader.prepareNativeTransferRuntimeConfiguration(ctx, who, d.request.TenantID, ownTransferPreparation(p.Configuration))
	if err != nil {
		return zero, nativeAllTargetAdmissionError("runtime_configuration", err)
	}
	c.Scene, err = frozenSemanticSceneParentPolicy(ctx, d)
	if err != nil {
		return zero, nativeAllTargetAdmissionError("scene_policy", err)
	}
	selected, err := selectNativeQualificationPolicies(sourcePerson.policy, []string{"mask.person"})
	if err != nil {
		return zero, nativeAllTargetAdmissionError("person_policy", err)
	}
	c.PersonProtection = &TransferPersonProtection{Report: sourcePerson.report, Expected: selected["mask.person"]}
	if c.SourceProtection != (v2input.Ref{}) && c.SourceProtection != protection.verified.Root {
		return zero, nativeAllTargetAdmissionError("source_protection", v2input.ErrConflict)
	}
	c.SourceProtection = protection.verified.Root
	_, source, err := p.Reader.readQualifiedSourcePreparationProof(ctx, who, d.request.TenantID, d.request.Intent.Reference, c.Color, false)
	if err != nil {
		return zero, nativeAllTargetAdmissionError("source_proof", err)
	}
	person, err := p.Reader.prepareNativeInputReferencePersonMode(ctx, who, d.request, source, protection, c.Color, sourcePerson.report, true)
	if err != nil {
		return zero, nativeAllTargetAdmissionError("reference_person", err)
	}
	reference, err := p.Reader.readNativeInputTransferReferenceDocuments(ctx, who, d.request, person, c.Color, c.MetadataBytes)
	if err != nil {
		return zero, nativeAllTargetAdmissionError("reference_documents", err)
	}
	p.Configuration = c
	targets, err := p.restoreNativeTransferTargets(ctx, who, d, manifest, reference, catalogs)
	if err != nil {
		return zero, nativeAllTargetAdmissionError("targets", err)
	}
	c.TargetProtection = make([]v2input.Ref, len(targets))
	for i, target := range targets {
		c.TargetProtection[i] = target.protection.Root
	}
	c.sceneEvidence, err = p.prepareNativeSceneEvidence(ctx, who, d, c, catalogs)
	if err != nil {
		return zero, nativeAllTargetAdmissionError("scene_evidence", err)
	}
	inputs, documents, err := p.Reader.prepareNativeAllTargetTransferInputs(ctx, who, d.request, d.published, c, reference, targets)
	if err != nil {
		return zero, nativeAllTargetAdmissionError("input_documents", err)
	}
	params, err := PublishTransferParams(ctx, p.Documents, who, d.request, d.requestKey, d.published, c.Qualification.ModelLockSHA, c.Qualification.Calibration)
	if err != nil {
		return zero, nativeAllTargetAdmissionError("publish_params", err)
	}
	plan, err := p.Reader.compileNativeAllTargetTransferWorkflowPlan(ctx, who, d.request, d.published, d.anchor.snapshotRef, d.anchor.snapshotBody, params, inputs, documents, c)
	if err != nil {
		return zero, nativeAllTargetAdmissionError("compile_plan", err)
	}
	built, err := recoverNativeInputDefectTransport(ctx, p.Scheduler, who, defect)
	if err != nil {
		return zero, nativeAllTargetAdmissionError("recover_defect_transport", err)
	}
	publisher := v2input.ScopedPublisher{Documents: p.TransportDocuments, Tenant: d.request.TenantID, Run: d.request.RunID}
	prepared, err := p.Reader.persistNativeAllTargetTransfer(ctx, p.Scheduler, publisher, who, d.request, d.requestKey, d.published, plan, c, built, p.ReservedBytes)
	if err != nil {
		return zero, nativeAllTargetAdmissionError("persist_parent", err)
	}
	transports := make([]v2input.Built, len(prepared))
	for i, task := range prepared {
		transports[i] = task.Graph.Built
	}
	ticket, err := p.Reader.activateNativeAllTargetTransfer(ctx, p.Scheduler, who, d.request, d.requestKey, d.published, plan, c, built, transports)
	return ticket, nativeAllTargetAdmissionError("activate_parent", err)
}
