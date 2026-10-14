package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// Caller holds the original parent lock. A prerequisite never completes the
// parent handoff; every saved stage repeats physical dependency authority.
func (p TransferDraftPreparer) advanceNativeTargetSkins(ctx context.Context, who auth.Identity, d detectorDraftPlan, manifest nativeTargetPreparationManifest, catalogs NativeParentArtifactCatalogFactory) (nativeFacePipelineAdvance, error) {
	var zero nativeFacePipelineAdvance
	if catalogs == nil || p.Scheduler == nil || p.TransportDocuments == nil {
		return zero, v2input.ErrInvalid
	}
	if err := verifyDetectorPlanSeal(ctx, d); err != nil {
		return zero, err
	}
	owned, err := restoreNativeTargetPreparationManifest(ctx, who, d.request, d.requestKey, d.published, d.anchor.snapshotRef, d.anchor.snapshotBody, manifest.body, manifest.sha)
	if err != nil {
		return zero, err
	}
	var inventory nativeTargetPreparationManifestBody
	if json.Unmarshal(owned.body, &inventory) != nil {
		return zero, v2input.ErrInvalid
	}
	for _, entry := range inventory.Entries {
		if entry.ReuseReference {
			continue
		}
		a, err := compileNativeTargetAnchor(ctx, who, d.request, d.requestKey, d.published, d.anchor.snapshotRef, d.anchor.snapshotBody, owned, entry.Ordinal, "face.detect")
		if err != nil {
			return zero, err
		}
		catalog, err := catalogs(ctx, who, a.request.TenantID, p.Reader.ProjectID, a.entry.Binding.AssetVersionID)
		if err != nil {
			return zero, err
		}
		savedDetector, err := p.Reader.readNativeTargetDetectorSaved(ctx, who, a)
		if err != nil {
			return zero, err
		}
		if savedDetector.State != "saved" || savedDetector.RunState != "completed" {
			return zero, v2input.ErrConflict
		}
		detector, err := p.Reader.restoreNativeTargetDetectorPlan(ctx, catalog, who, a, p.Configuration.Color, savedDetector)
		if err != nil {
			return zero, err
		}
		var detectorBuilt v2input.Built
		if err := database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
			var err error
			detectorBuilt, err = p.Reader.recoverNativeTargetDetectorTransport(ctx, tx, who, d, detector)
			if err != nil {
				return err
			}
			_, err = p.Reader.sealNativeTargetDetectorInTransaction(ctx, tx, who, d, detector, detectorBuilt)
			return err
		}); err != nil {
			return zero, err
		}
		parserAnchor, err := compileNativeTargetAnchor(ctx, who, d.request, d.requestKey, d.published, d.anchor.snapshotRef, d.anchor.snapshotBody, owned, entry.Ordinal, "face.parse")
		if err != nil {
			return zero, err
		}
		savedParser, err := p.Reader.readNativeTargetFaceSaved(ctx, who, parserAnchor)
		if err != nil {
			return zero, err
		}
		if savedParser.State != "saved" || savedParser.RunState != "completed" {
			return zero, v2input.ErrConflict
		}
		parser, err := p.Reader.restoreNativeTargetFacePlan(ctx, catalog, who, detector, parserAnchor, p.Configuration.Color, savedParser)
		if err != nil {
			return zero, err
		}
		var parserBuilt v2input.Built
		if err := database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
			var err error
			parserBuilt, err = p.Reader.recoverNativeTargetFaceTransport(ctx, tx, who, d, parser)
			if err != nil {
				return err
			}
			_, err = p.Reader.sealNativeTargetFaceInTransaction(ctx, tx, who, d, parser, parserBuilt, detectorBuilt)
			return err
		}); err != nil {
			return zero, err
		}
		a, err = compileNativeTargetAnchor(ctx, who, d.request, d.requestKey, d.published, d.anchor.snapshotRef, d.anchor.snapshotBody, owned, entry.Ordinal, "mask.face_skin")
		if err != nil {
			return zero, err
		}
		saved, err := p.Reader.readNativeTargetSkinSaved(ctx, who, a)
		if err != nil {
			return zero, err
		}
		waiting := nativeFacePipelineAdvance{phase: nativePreparationWaiting, operation: "mask.face_skin"}
		var plan nativeTargetSkinDraftPlan
		if saved.State == "absent" {
			cfg, err := p.Reader.selectInitialFacePipelineConfiguration(ctx, who, d.request.TenantID, p.Configuration, p.ReservedBytes, p.TransportDocuments)
			if err != nil {
				return zero, err
			}
			params, err := p.Reader.publishNativeTargetSkinParams(ctx, who, parser, p.Configuration.Color, p.Documents)
			if err != nil {
				return zero, err
			}
			task, err := p.Reader.prepareNativeTargetSkinTask(ctx, who, parser, p.Configuration.Color, params, cfg.Skin.RAM, cfg.Skin.Scratch, cfg.Skin.Output)
			if err != nil {
				return zero, err
			}
			plan, err = p.Reader.compileNativeTargetSkinDraftPlan(ctx, who, parser, task, cfg.Skin.Reserved)
			if err != nil {
				return zero, err
			}
			if err := database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
				_, err := p.Reader.beginNativeTargetSkinDraftInTransaction(ctx, tx, who, d, plan, p.Configuration.Color, parserBuilt, detectorBuilt)
				return err
			}); err != nil {
				return zero, err
			}
		} else {
			plan, err = p.Reader.restoreNativeTargetSkinPlan(ctx, who, parser, a, p.Configuration.Color, saved)
			if err != nil {
				return zero, err
			}
		}
		if !saved.Sealed {
			transport, _, err := publishNativeTargetSkinDraftTransport(ctx, plan, v2input.ScopedPublisher{Documents: p.TransportDocuments, Tenant: d.request.TenantID, Run: plan.shell.Run})
			if err != nil {
				return zero, err
			}
			if err := database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
				if err := p.Reader.registerNativeTargetSkinInputsInTransaction(ctx, tx, who, d, plan, p.Configuration.Color, parserBuilt, detectorBuilt, transport.Graph.Built); err != nil {
					return err
				}
				_, err := p.Reader.sealNativeTargetSkinInTransaction(ctx, tx, who, d, plan, transport.Graph.Built, parserBuilt, detectorBuilt)
				return err
			}); err != nil {
				return zero, err
			}
			return waiting, nil
		}
		phase, err := nativeInputStagePreparationPhase(nativeInputSavedStageState{task: saved.Task, taskState: saved.TaskState, runState: saved.RunState, sealed: saved.Sealed})
		if err != nil {
			return zero, err
		}
		var built v2input.Built
		if err := database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
			var err error
			built, err = p.Reader.recoverNativeTargetSkinTransport(ctx, tx, who, d, plan)
			if err != nil {
				return err
			}
			_, err = p.Reader.sealNativeTargetSkinInTransaction(ctx, tx, who, d, plan, built, parserBuilt, detectorBuilt)
			return err
		}); err != nil {
			return zero, err
		}
		if phase == nativePreparationWaiting {
			return waiting, nil
		}
		if phase != nativePreparationSucceeded {
			return zero, v2input.ErrConflict
		}
		proof, err := p.Reader.readCompletedNativeTargetSkin(ctx, who, plan, p.Configuration.Color)
		if err != nil {
			return zero, err
		}
		packet, err := p.Reader.marshalNativeTargetSkinCompletedEvidence(ctx, who, d, plan, p.Configuration.Color, proof, built, parserBuilt, detectorBuilt)
		if err != nil {
			return zero, err
		}
		if err := database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
			return p.Reader.finishNativeTargetSkinInTransaction(ctx, tx, who, plan, packet)
		}); err != nil {
			return zero, err
		}
	}
	return nativeFacePipelineAdvance{phase: nativePreparationWaiting, operation: "semantic.transfer"}, ctx.Err()
}
