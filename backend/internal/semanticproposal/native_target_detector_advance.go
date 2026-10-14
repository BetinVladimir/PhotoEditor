package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// Caller holds original parent advisory lock. Each target uses its own immutable
// namespace; incomplete later prerequisite stages keep the original handoff live.
func (p TransferDraftPreparer) advanceNativeTargetDetectors(ctx context.Context, who auth.Identity, d detectorDraftPlan, manifest nativeTargetPreparationManifest, catalogs NativeParentArtifactCatalogFactory) (nativeFacePipelineAdvance, error) {
	waiting := nativeFacePipelineAdvance{phase: nativePreparationWaiting, operation: "face.detect"}
	if catalogs == nil || p.Scheduler == nil || p.TransportDocuments == nil {
		return nativeFacePipelineAdvance{}, v2input.ErrInvalid
	}
	if err := verifyDetectorPlanSeal(ctx, d); err != nil {
		return nativeFacePipelineAdvance{}, err
	}
	owned, err := restoreNativeTargetPreparationManifest(ctx, who, d.request, d.requestKey, d.published, d.anchor.snapshotRef, d.anchor.snapshotBody, manifest.body, manifest.sha)
	if err != nil {
		return nativeFacePipelineAdvance{}, err
	}
	var inventory nativeTargetPreparationManifestBody
	if json.Unmarshal(owned.body, &inventory) != nil {
		return nativeFacePipelineAdvance{}, v2input.ErrInvalid
	}
	for _, entry := range inventory.Entries {
		if entry.ReuseReference {
			continue
		}
		a, err := compileNativeTargetAnchor(ctx, who, d.request, d.requestKey, d.published, d.anchor.snapshotRef, d.anchor.snapshotBody, manifest, entry.Ordinal, "face.detect")
		if err != nil {
			return nativeFacePipelineAdvance{}, err
		}
		saved, err := p.Reader.readNativeTargetDetectorSaved(ctx, who, a)
		if err != nil {
			return nativeFacePipelineAdvance{}, err
		}
		catalog, err := catalogs(ctx, who, a.request.TenantID, p.Reader.ProjectID, a.entry.Binding.AssetVersionID)
		if err != nil {
			return nativeFacePipelineAdvance{}, err
		}
		var plan nativeTargetDetectorPlan
		if saved.State == "absent" {

			metadata, err := nativePlanRecoveryFromTask(d.shell.Tasks[0], &d.prepared.qualification.report.ref, d.prepared.costs, d.shell.Bytes)
			if err != nil {
				return nativeFacePipelineAdvance{}, err
			}
			budget := nativePreparationBudget{Costs: metadata.Costs, RAM: metadata.RAM, VRAM: metadata.VRAM, Scratch: metadata.Scratch, Output: metadata.Output, Reserved: metadata.Reserved}

			originalTask := d.shell.Tasks[0]
			originalParams := v2input.Ref{ArtifactID: originalTask.ParamsArtifactID, SHA256: originalTask.ParamsSHA}
			originalObject, err := p.Reader.defectVersionParamsObject(ctx, who, d.request.TenantID, d.request.Intent.Reference, d.prepared.document, originalParams)
			if err != nil {
				return nativeFacePipelineAdvance{}, err
			}
			raw, err := v2input.ReadPinnedSemanticDocument(ctx, p.Reader.Objects, originalObject, originalParams)
			if err != nil {
				return nativeFacePipelineAdvance{}, err
			}
			var options struct {
				Score   float64 `json:"score_threshold"`
				Maximum int     `json:"max_faces"`
			}
			if json.Unmarshal(raw, &options) != nil {
				return nativeFacePipelineAdvance{}, v2input.ErrInvalid
			}
			currentObject, err := p.Reader.defectVersionParamsObject(ctx, who, d.request.TenantID, d.request.Intent.Reference, d.prepared.document, originalParams)
			if err != nil {
				return nativeFacePipelineAdvance{}, err
			}
			if currentObject != originalObject {
				return nativeFacePipelineAdvance{}, v2input.ErrConflict
			}
			plan, err = p.Reader.prepareNativeTargetDetectorPlan(ctx, catalog, p.Documents, who, a, p.Configuration.Color, d.prepared.qualification.report.ref, options.Score, options.Maximum, budget)
			if err != nil {
				return nativeFacePipelineAdvance{}, err
			}
			if err := database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
				_, err := p.Reader.beginNativeTargetDetectorInTransaction(ctx, tx, who, d, plan, p.Configuration.Color)
				return err
			}); err != nil {
				return nativeFacePipelineAdvance{}, err
			}
		} else {
			plan, err = p.Reader.restoreNativeTargetDetectorPlan(ctx, catalog, who, a, p.Configuration.Color, saved)
			if err != nil {
				return nativeFacePipelineAdvance{}, err
			}
		}
		if !saved.Sealed {
			transport, _, err := publishNativeTargetDetectorTransport(ctx, plan, v2input.ScopedPublisher{Documents: p.TransportDocuments, Tenant: a.request.TenantID, Run: plan.detector.shell.Run})
			if err != nil {
				return nativeFacePipelineAdvance{}, err
			}
			if err := database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
				if err := p.Reader.registerNativeTargetDetectorInTransaction(ctx, tx, who, d, plan, p.Configuration.Color, transport.Graph.Built); err != nil {
					return err
				}
				_, err := p.Reader.sealNativeTargetDetectorInTransaction(ctx, tx, who, d, plan, transport.Graph.Built)
				return err
			}); err != nil {
				return nativeFacePipelineAdvance{}, err
			}
			return waiting, nil
		}
		phase, err := nativeInputStagePreparationPhase(nativeInputSavedStageState{task: saved.Task, taskState: saved.TaskState, runState: saved.RunState, sealed: saved.Sealed})
		if err != nil {
			return nativeFacePipelineAdvance{}, err
		}
		var built v2input.Built
		if err := database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
			var err error
			built, err = p.Reader.recoverNativeTargetDetectorTransport(ctx, tx, who, d, plan)
			if err != nil {
				return err
			}
			_, err = p.Reader.sealNativeTargetDetectorInTransaction(ctx, tx, who, d, plan, built)
			return err
		}); err != nil {
			return nativeFacePipelineAdvance{}, err
		}
		if phase == nativePreparationWaiting {
			return waiting, nil
		}
		if phase != nativePreparationSucceeded {
			return nativeFacePipelineAdvance{}, v2input.ErrConflict
		}
		result, err := p.Reader.readNativeTargetDetectorResult(ctx, who, plan, p.Configuration.Color)
		if err != nil {
			return nativeFacePipelineAdvance{}, err
		}
		if err := database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
			return p.Reader.finishNativeTargetDetectorInTransaction(ctx, tx, who, d, plan, built, result)
		}); err != nil {
			return nativeFacePipelineAdvance{}, err
		}
	}
	return nativeFacePipelineAdvance{phase: nativePreparationWaiting, operation: "semantic.transfer"}, ctx.Err()
}
