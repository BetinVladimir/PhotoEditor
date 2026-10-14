package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// Server entry point: owns the parent advisory lock and loads every document
// and private proof. Incomplete blocked transport uses the scoped publisher;
// complete/sealed graphs remain SQL-only replay without current capacity defaults.
func (p TransferDraftPreparer) loadAndResumeNativeAllTargetTransfer(ctx context.Context, who auth.Identity, tenant uuid.UUID, key string, body []byte, color TransferColorProfile) (v2input.JobTicket, error) {
	var zero, ticket v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if p.Scheduler == nil || p.Reader.DB == nil || p.Reader.Objects == nil || p.Reader.ProjectID == uuid.Nil || p.Reader.Bucket == "" || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(color.sha); err != nil {
		return zero, v2input.ErrInvalid
	}
	request, err := CompileRequest(tenant, who.UserID, key, body)
	if err != nil {
		return zero, err
	}
	owned, err := withNativeManualParentLock(ctx, p.Scheduler, tenant, request.RunID, func(work context.Context) error {
		ticket, err = p.loadAndResumeNativeAllTargetTransferLocked(work, who, request, key, color)
		return err
	})
	if err != nil {
		return zero, err
	}
	if !owned {
		return zero, v2input.ErrConflict
	}
	return ticket, nil
}

// Caller must hold the original parent advisory lock. Recovery callbacks use
// this entry to avoid recursively acquiring that lock; all live proofs and SQL
// admission checks are still repeated by the loader and resume.
func (p TransferDraftPreparer) loadAndResumeNativeAllTargetTransferLocked(ctx context.Context, who auth.Identity, request Request, key string, color TransferColorProfile) (v2input.JobTicket, error) {
	var zero v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if p.Scheduler == nil || p.Reader.DB == nil || p.Reader.Objects == nil || p.Reader.ProjectID == uuid.Nil || p.Reader.Bucket == "" || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(color.sha); err != nil {
		return zero, v2input.ErrInvalid
	}
	raw, sha, err := p.Reader.readNativeAllTargetTransferSaved(ctx, who, request)
	if err != nil {
		return zero, err
	}
	saved, err := p.Reader.decodeNativeAllTargetTransferSaved(ctx, who, request, raw, sha)
	if err != nil {
		return zero, err
	}
	budget := saved.Native.Capacity.Metadata
	published, err := p.Reader.readNativeInputSavedPublishedIntent(ctx, who, request, budget)
	if err != nil {
		return zero, err
	}
	metadata := nativeInputTransferSavedMetadata{snapshot: *saved.Semantic.Snapshot, snapshotDigest: saved.Semantic.SnapshotDigest, configSHA: sha, params: make([]v2input.Ref, len(saved.Semantic.Nodes))}
	for i, node := range saved.Semantic.Nodes {
		metadata.params[i] = v2input.Ref{ArtifactID: node.ParamsArtifactID, SHA256: node.ParamsSHA}
	}
	snapshot, err := p.Reader.readNativeInputTransferSavedSnapshot(ctx, who, request, metadata, budget)
	if err != nil {
		return zero, err
	}
	var retained int64
	if err := chargeMetadata(&retained, budget, len(raw), len(snapshot), len(metadata.params)*512, len(published.Inventory.Targets)*512); err != nil {
		return zero, err
	}
	factory := (SQLNativeParentArtifactCatalogFactory{API: p.Reader.DB, Bucket: p.Reader.Bucket}).ForReference
	catalog, err := factory(ctx, who, request.TenantID, p.Reader.ProjectID, request.Intent.Reference.AssetVersionID)
	if err != nil {
		return zero, err
	}
	original, reference, built, err := p.restoreNativeAllTargetTransferOriginal(ctx, catalog, who, request, key, published, metadata.snapshot, snapshot, color)
	if err != nil {
		return zero, err
	}
	plan, capacity, err := p.restoreNativeAllTargetTransferPlan(ctx, who, original, reference, color, factory)
	if err != nil {
		return zero, err
	}
	if err := p.repairNativeAllTargetTransfer(ctx, who, request, published, plan, capacity, built); err != nil {
		return zero, err
	}
	ticket, err := p.Reader.resumeNativeAllTargetTransfer(ctx, p.Scheduler, who, request, key, published, plan, capacity, built)
	if err != nil {
		return zero, err
	}
	return ticket, nil
}
