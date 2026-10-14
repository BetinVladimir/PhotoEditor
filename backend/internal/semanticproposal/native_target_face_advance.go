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
func (p TransferDraftPreparer) advanceNativeTargetFaces(ctx context.Context, who auth.Identity, d detectorDraftPlan, manifest nativeTargetPreparationManifest, catalogs NativeParentArtifactCatalogFactory) (nativeFacePipelineAdvance, error) {
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
		result, err := p.Reader.readNativeTargetDetectorResult(ctx, who, detector, p.Configuration.Color)
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
		for _, op := range []string{"face.landmarks", "face.parse"} {
			waiting := nativeFacePipelineAdvance{phase: nativePreparationWaiting, operation: op}
			faceAnchor, err := compileNativeTargetAnchor(ctx, who, d.request, d.requestKey, d.published, d.anchor.snapshotRef, d.anchor.snapshotBody, owned, entry.Ordinal, op)
			if err != nil {
				return zero, err
			}
			saved, err := p.Reader.readNativeTargetFaceSaved(ctx, who, faceAnchor)
			if err != nil {
				return zero, err
			}
			var plan nativeTargetFaceDraftPlan
			if saved.State == "absent" {
				cfg, err := p.Reader.selectInitialFacePipelineConfiguration(ctx, who, d.request.TenantID, p.Configuration, p.ReservedBytes, p.TransportDocuments)
				if err != nil {
					return zero, err
				}
				stage := cfg.Landmarks
				if op == "face.parse" {
					stage = cfg.Parser
				}
				params, err := p.Reader.publishNativeTargetFaceStageParams(ctx, who, detector, p.Configuration.Color, p.Documents, op)
				if err != nil {
					return zero, err
				}
				task, err := p.Reader.prepareNativeTargetFaceTask(ctx, catalog, who, detector, p.Configuration.Color, params, stage.Report, stage.Budget)
				if err != nil {
					return zero, err
				}
				plan, err = p.Reader.compileNativeTargetFaceDraftPlan(ctx, who, detector, result, task, stage.Budget.Reserved)
				if err != nil {
					return zero, err
				}
				if err := database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
					_, err := p.Reader.beginNativeTargetFaceDraftInTransaction(ctx, tx, who, d, plan, p.Configuration.Color, detectorBuilt)
					return err
				}); err != nil {
					return zero, err
				}
			} else {
				plan, err = p.Reader.restoreNativeTargetFacePlan(ctx, catalog, who, detector, faceAnchor, p.Configuration.Color, saved)
				if err != nil {
					return zero, err
				}
			}
			if !saved.Sealed {
				transport, _, err := publishNativeTargetFaceDraftTransport(ctx, plan, v2input.ScopedPublisher{Documents: p.TransportDocuments, Tenant: d.request.TenantID, Run: plan.shell.Run})
				if err != nil {
					return zero, err
				}
				if err := database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
					if err := p.Reader.registerNativeTargetFaceInputsInTransaction(ctx, tx, who, d, plan, p.Configuration.Color, detectorBuilt, transport.Graph.Built); err != nil {
						return err
					}
					_, err := p.Reader.sealNativeTargetFaceInTransaction(ctx, tx, who, d, plan, transport.Graph.Built, detectorBuilt)
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
				built, err = p.Reader.recoverNativeTargetFaceTransport(ctx, tx, who, d, plan)
				if err != nil {
					return err
				}
				_, err = p.Reader.sealNativeTargetFaceInTransaction(ctx, tx, who, d, plan, built, detectorBuilt)
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
			proof, err := p.Reader.readNativeTargetFaceResult(ctx, who, plan, p.Configuration.Color)
			if err != nil {
				return zero, err
			}
			if err := database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
				return p.Reader.finishNativeTargetFaceInTransaction(ctx, tx, who, d, plan, built, detectorBuilt, proof)
			}); err != nil {
				return zero, err
			}
		}
	}
	return nativeFacePipelineAdvance{phase: nativePreparationWaiting, operation: "semantic.transfer"}, ctx.Err()
}
