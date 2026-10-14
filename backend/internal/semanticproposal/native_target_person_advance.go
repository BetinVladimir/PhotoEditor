package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Caller holds original parent advisory lock. Each target uses its own immutable
// namespace; incomplete later prerequisite stages keep the original handoff live.
func (p TransferDraftPreparer) advanceNativeTargetPersons(ctx context.Context, who auth.Identity, d detectorDraftPlan, manifest nativeTargetPreparationManifest, catalogs NativeParentArtifactCatalogFactory) (nativeFacePipelineAdvance, error) {
	waiting := nativeFacePipelineAdvance{phase: nativePreparationWaiting, operation: "mask.person"}
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
		a, err := compileNativeTargetAnchor(ctx, who, d.request, d.requestKey, d.published, d.anchor.snapshotRef, d.anchor.snapshotBody, manifest, entry.Ordinal, "mask.person")
		if err != nil {
			return nativeFacePipelineAdvance{}, err
		}
		saved, err := p.Reader.readNativeTargetPersonSaved(ctx, who, a)
		if err != nil {
			return nativeFacePipelineAdvance{}, err
		}
		catalog, err := catalogs(ctx, who, a.request.TenantID, p.Reader.ProjectID, a.entry.Binding.AssetVersionID)
		if err != nil {
			return nativeFacePipelineAdvance{}, err
		}
		var plan nativeTargetPersonPlan
		if saved.State == "absent" {
			selected, err := p.Reader.selectNativePersonProtectionPolicy(ctx, who, a.request.TenantID, p.Configuration.MetadataBytes)
			if err != nil {
				return nativeFacePipelineAdvance{}, err
			}
			pin, err := p.Reader.personQualificationObject(ctx, who, a.request.TenantID, selected.Expected, selected.Report)
			if err != nil {
				return nativeFacePipelineAdvance{}, err
			}
			raw, err := v2input.ReadPinnedSemanticDocument(ctx, p.Reader.Objects, pin.object, selected.Report)
			if err != nil {
				return nativeFacePipelineAdvance{}, err
			}
			report, err := v2qualification.Verify(raw, selected.Report.SHA256, selected.Expected)
			if err != nil {
				return nativeFacePipelineAdvance{}, err
			}
			budget, err := createPersonBudget(p.Configuration, p.ReservedBytes, report.Metrics)
			if err != nil {
				return nativeFacePipelineAdvance{}, err
			}
			plan, err = p.Reader.prepareNativeTargetPersonPlan(ctx, catalog, p.Documents, who, a, p.Configuration.Color, selected.Report, 512, budget)
			if err != nil {
				return nativeFacePipelineAdvance{}, err
			}
			if err := database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
				_, err := p.Reader.beginNativeTargetPersonInTransaction(ctx, tx, who, d, plan, p.Configuration.Color)
				return err
			}); err != nil {
				return nativeFacePipelineAdvance{}, err
			}
		} else {
			plan, err = p.Reader.restoreNativeTargetPersonPlan(ctx, catalog, who, a, p.Configuration.Color, saved)
			if err != nil {
				return nativeFacePipelineAdvance{}, err
			}
		}
		if !saved.Sealed {
			transport, _, err := publishNativeTargetPersonTransport(ctx, plan, v2input.ScopedPublisher{Documents: p.TransportDocuments, Tenant: a.request.TenantID, Run: plan.person.shell.Run})
			if err != nil {
				return nativeFacePipelineAdvance{}, err
			}
			if err := database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
				if err := p.Reader.registerNativeTargetPersonInTransaction(ctx, tx, who, d, plan, p.Configuration.Color, transport.Graph.Built); err != nil {
					return err
				}
				_, err := p.Reader.sealNativeTargetPersonInTransaction(ctx, tx, who, d, plan, transport.Graph.Built)
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
			built, err = p.Reader.recoverNativeTargetPersonTransport(ctx, tx, who, d, plan)
			if err != nil {
				return err
			}
			_, err = p.Reader.sealNativeTargetPersonInTransaction(ctx, tx, who, d, plan, built)
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
		result, err := p.Reader.readNativeTargetPersonResult(ctx, who, plan)
		if err != nil {
			return nativeFacePipelineAdvance{}, err
		}
		if err := database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
			return p.Reader.finishNativeTargetPersonInTransaction(ctx, tx, who, d, plan, built, result)
		}); err != nil {
			return nativeFacePipelineAdvance{}, err
		}
	}
	return nativeFacePipelineAdvance{phase: nativePreparationWaiting, operation: "semantic.transfer"}, ctx.Err()
}
