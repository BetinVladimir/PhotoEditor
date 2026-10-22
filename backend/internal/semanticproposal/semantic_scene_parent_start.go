package semanticproposal

import (
	"context"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Start the actual existing qualified original detector pipeline before scene
// prerequisites. The durable initial recovery coordinator owns every later
// stage, including person/face/protection and final parent acceptance. Exact
// public receipt replay happens before this method in CreateService.Propose.
func (p TransferDraftPreparer) prepareOrdinarySceneParent(ctx context.Context, who auth.Identity, request Request, key string) error {
	return p.prepareOrdinarySceneParentMode(ctx, who, request, key, false)
}

// ownsParentLock is used only by the existing ordinary recovery callback, which
// already holds the same session advisory lock. Nested acquisition on a second
// pooled connection must not deadlock the accepted command's recovery.
func (p TransferDraftPreparer) prepareOrdinarySceneParentMode(ctx context.Context, who auth.Identity, request Request, key string, ownsParentLock bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.Configuration.Scene == nil || p.Configuration.PersonProtection == nil || p.Scheduler == nil || p.TransportDocuments == nil {
		return v2input.ErrInvalid
	}
	p.Reader.scenePolicy = ordinaryParentPolicy(p).Scene
	catalog := v2draft.SQLArtifactCatalog{DB: p.Reader.DB, Bucket: p.Reader.Bucket, ProjectID: p.Reader.ProjectID, AssetVersionID: request.Intent.Reference.AssetVersionID}
	detectorRun, _, err := detectorPreparationIDs(request.RunID)
	if err != nil {
		return err
	}
	bootstrap := nativePreparationBootstrap{Reader: p.Reader, Scheduler: p.Scheduler, Catalog: catalog, Documents: p.Documents, Transport: v2input.ScopedPublisher{Documents: p.TransportDocuments, Tenant: request.TenantID, Run: detectorRun}}
	if err := bootstrap.validate(ctx); err != nil {
		return err
	}
	advance := func(work context.Context) error {
		presence, err := p.Reader.readInitialNativeDetectorPresence(work, who, request, key)
		if err != nil {
			return err
		}
		switch presence {
		case nativeStagePresent:
			// The recovery service reads the frozen original snapshot and policy.
			// Never replace a saved original anchor with current defaults here.
			return nil
		case nativeStageAbsent:
		default:
			return v2input.ErrConflict
		}
		// Refuse a new expensive prerequisite chain when its selected final
		// scientific policy is not actually qualified. Existing checkpoints above
		// retain their own frozen recovery policy and are not replaced here.
		if _, err := p.Reader.readSemanticScenePolicy(work, who, request.TenantID, p.Configuration.Qualification, p.Configuration.Expected, p.Configuration.Scene.Policy); err != nil {
			return err
		}
		for _, producer := range []TransferSceneProducer{p.Configuration.Scene.Scene, p.Configuration.Scene.Depth} {
			if _, err := p.Reader.readSceneProducerQualification(work, who, request.TenantID, producer); err != nil {
				return err
			}
		}
		anchor, err := bootstrap.prepareInitialNativeAnchor(work, who, request.TenantID, key, request.Body, p.Configuration, p.ReservedBytes)
		if err != nil {
			return err
		}
		// Actual FaceDetectParams defaults, then frozen in canonical Params. These
		// do not create qualification; the ordinary installed producer still must
		// pass the independently verified current policy/report checks.
		_, _, err = bootstrap.bootstrapQualifiedInitialDetector(work, who, anchor, p.Configuration, p.ReservedBytes, p.TransportDocuments, .8, 64)
		return err
	}
	locked := ownsParentLock
	if ownsParentLock {
		err = advance(ctx)
	} else {
		locked, err = withNativeManualParentLock(ctx, p.Scheduler, request.TenantID, request.RunID, advance)
	}
	if err != nil {
		return err
	}
	if !locked {
		return v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return ordinaryScenePreparationPending()
}
