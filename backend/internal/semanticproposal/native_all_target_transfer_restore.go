package semanticproposal

import (
	"context"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Caller holds original parent lock and independently restores originalPinned
// reference first. No publishers or current resource defaults are consulted.
func (p TransferDraftPreparer) restoreNativeAllTargetTransferPlan(ctx context.Context, who auth.Identity, original detectorDraftPlan, reference nativeInputTransferReferenceDocuments, color TransferColorProfile, catalogs NativeParentArtifactCatalogFactory) (nativeAllTargetTransferWorkflowPlan, TransferPreparation, error) {
	var zero nativeAllTargetTransferWorkflowPlan
	var empty TransferPreparation
	if err := ctx.Err(); err != nil {
		return zero, empty, err
	}
	if p.Scheduler == nil || catalogs == nil {
		return zero, empty, v2input.ErrInvalid
	}
	raw, sha, err := p.Reader.readNativeAllTargetTransferSaved(ctx, who, original.request)
	if err != nil {
		return zero, empty, err
	}
	saved, err := p.Reader.decodeNativeAllTargetTransferSaved(ctx, who, original.request, raw, sha)
	if err != nil {
		return zero, empty, err
	}
	c, err := p.Reader.restoreNativeAllTargetTransferCapacity(ctx, who, original.request, raw, sha, reference, color)
	if err != nil {
		return zero, empty, err
	}
	metadata := nativeInputTransferSavedMetadata{snapshot: *saved.Semantic.Snapshot, snapshotDigest: saved.Semantic.SnapshotDigest, configSHA: sha, params: make([]v2input.Ref, len(saved.Semantic.Nodes))}
	for i, node := range saved.Semantic.Nodes {
		metadata.params[i] = v2input.Ref{ArtifactID: node.ParamsArtifactID, SHA256: node.ParamsSHA}
	}
	snapshot, err := p.Reader.readNativeInputTransferSavedSnapshot(ctx, who, original.request, metadata, c.MetadataBytes)
	if err != nil {
		return zero, empty, err
	}
	if err := p.Reader.verifyNativeInputTransferSavedParams(ctx, who, original.request, original.published, metadata, c); err != nil {
		return zero, empty, err
	}
	manifest, err := compileNativeTargetPreparationManifest(ctx, who, original.request, original.requestKey, original.published, original.anchor.snapshotRef, original.anchor.snapshotBody)
	if err != nil {
		return zero, empty, err
	}
	p.Configuration = ownTransferPreparation(c)
	if err:=p.verifyFrozenNativeSceneEvidence(ctx,who,original,c,catalogs);err!=nil{return zero,empty,err}
	targets, err := p.restoreNativeTransferTargets(ctx, who, original, manifest, reference, catalogs)
	if err != nil {
		return zero, empty, err
	}
	inputs, documents, err := p.Reader.prepareNativeAllTargetTransferInputs(ctx, who, original.request, original.published, c, reference, targets)
	if err != nil {
		return zero, empty, err
	}
	plan, err := p.Reader.compileNativeAllTargetTransferWorkflowPlan(ctx, who, original.request, original.published, metadata.snapshot, snapshot, metadata.params, inputs, documents, c)
	if err != nil {
		return zero, empty, err
	}
	if plan.core.configSHA != sha {
		return zero, empty, v2input.ErrConflict
	}
	if err := p.Reader.validateNativeAllTargetTransferPlanScope(ctx, who, original.request, plan, c); err != nil {
		return zero, empty, err
	}
	after, afterSHA, err := p.Reader.readNativeAllTargetTransferSaved(ctx, who, original.request)
	if err != nil {
		return zero, empty, err
	}
	if afterSHA != sha {
		return zero, empty, v2input.ErrConflict
	}
	if _, err := p.Reader.decodeNativeAllTargetTransferSaved(ctx, who, original.request, after, afterSHA); err != nil {
		return zero, empty, err
	}
	return plan, ownTransferPreparation(c), ctx.Err()
}
