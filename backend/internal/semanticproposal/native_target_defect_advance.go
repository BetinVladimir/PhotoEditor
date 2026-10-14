package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// Caller holds original parent lock. Object IO never spans a SQL transaction.
// Completed targets remain prerequisites, not parent transfer acceptance.
func (p TransferDraftPreparer) advanceNativeTargetDefects(ctx context.Context, who auth.Identity, d detectorDraftPlan, manifest nativeTargetPreparationManifest, catalogs NativeParentArtifactCatalogFactory) (nativeFacePipelineAdvance, error) {
	var zero nativeFacePipelineAdvance
	if catalogs == nil || p.Scheduler == nil || p.TransportDocuments == nil || p.Documents.DB == nil || p.Documents.Objects == nil {
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
		prerequisites, err := p.restoreNativeTargetDefectPrerequisites(ctx, who, d, owned, entry.Ordinal, catalogs)
		if err != nil {
			return zero, err
		}
		a, err := compileNativeTargetAnchor(ctx, who, d.request, d.requestKey, d.published, d.anchor.snapshotRef, d.anchor.snapshotBody, owned, entry.Ordinal, "defects.propose")
		if err != nil {
			return zero, err
		}
		saved, err := p.Reader.readNativeTargetDefectSaved(ctx, who, a)
		if err != nil {
			return zero, err
		}
		var plan nativeTargetDefectDraftPlan
		if saved.State == "absent" {
			cfg, err := p.Reader.selectInitialDefectConfiguration(ctx, who, d.request.TenantID, p.Configuration, p.ReservedBytes)
			if err != nil {
				return zero, err
			}
			prepared, err := p.Reader.prepareNativeTargetDefectParams(ctx, who, prerequisites.landmarks, prerequisites.skin, prerequisites.person, p.Configuration.Color, [2]float64{.002, .012})
			if err != nil {
				return zero, err
			}
			qualification, err := p.Reader.prepareNativeTargetDefectQualification(ctx, who, prepared, p.Configuration.Color, cfg.Report, cfg.Budget.Costs)
			if err != nil {
				return zero, err
			}
			params, err := p.Reader.publishNativeTargetDefectParams(ctx, who, prepared, p.Configuration.Color, p.Documents)
			if err != nil {
				return zero, err
			}
			task, err := p.Reader.prepareNativeTargetDefectTask(ctx, who, prepared, qualification, p.Configuration.Color, params, cfg.Budget.RAM, cfg.Budget.Scratch, cfg.Budget.Output)
			if err != nil {
				return zero, err
			}
			plan, err = p.Reader.compileNativeTargetDefectDraftPlan(ctx, who, task, cfg.Budget.Reserved)
			if err != nil {
				return zero, err
			}
		} else {
			plan, err = p.Reader.restoreNativeTargetDefectPlan(ctx, who, prerequisites.landmarks, prerequisites.skin, prerequisites.person, a, p.Configuration.Color, saved)
			if err != nil {
				return zero, err
			}
		}
		dependencies, err := p.Reader.prepareNativeTargetDefectDependencies(ctx, who, d, plan.prepared.preparation, p.Configuration.Color, prerequisites.transports)
		if err != nil {
			return zero, err
		}
		waiting := nativeFacePipelineAdvance{phase: nativePreparationWaiting, operation: "defects.propose"}
		if !saved.Sealed {
			if err := database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
				_, err := p.Reader.beginNativeTargetDefectDraftInTransaction(ctx, tx, who, d, plan, dependencies, p.Configuration.Color)
				return err
			}); err != nil {
				return zero, err
			}
			transport, _, err := p.Reader.publishOwnedNativeTargetDefectTransport(ctx, who, d, plan, p.Configuration.Color, v2input.ScopedPublisher{Documents: p.TransportDocuments, Tenant: plan.shell.Tenant, Run: plan.shell.Run})
			if err != nil {
				return zero, err
			}
			if err := database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
				if err := p.Reader.registerNativeTargetDefectInputsInTransaction(ctx, tx, who, d, plan, dependencies, p.Configuration.Color, transport.Graph.Built); err != nil {
					return err
				}
				_, err := p.Reader.sealNativeTargetDefectInTransaction(ctx, tx, who, d, plan, dependencies, transport.Graph.Built)
				return err
			}); err != nil {
				return zero, err
			}
			return waiting, nil
		}
		var built v2input.Built
		if err := database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
			var err error
			built, err = p.Reader.recoverNativeTargetDefectTransport(ctx, tx, who, d, plan)
			if err != nil {
				return err
			}
			_, err = p.Reader.sealNativeTargetDefectInTransaction(ctx, tx, who, d, plan, dependencies, built)
			return err
		}); err != nil {
			return zero, err
		}
		phase, err := nativeInputStagePreparationPhase(nativeInputSavedStageState{task: saved.Task, taskState: saved.TaskState, runState: saved.RunState, sealed: saved.Sealed})
		if err != nil {
			return zero, err
		}
		if phase == nativePreparationWaiting {
			return waiting, nil
		}
		if phase != nativePreparationSucceeded {
			return zero, v2input.ErrConflict
		}
		completed, err := p.Reader.readCompletedNativeTargetDefect(ctx, who, plan, p.Configuration.Color)
		if err != nil {
			return zero, err
		}
		packet, err := p.Reader.marshalNativeTargetDefectCompletedEvidence(ctx, who, d, plan, dependencies, p.Configuration.Color, completed, built)
		if err != nil {
			return zero, err
		}
		if err := database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
			if err := p.Reader.finishNativeTargetDefectInTransaction(ctx, tx, who, d, plan, dependencies, built, completed.catalog.ResultID, completed.catalog.Outputs); err != nil {
				return err
			}
			return p.Reader.lockNativeCompletedTargetDefectInTransaction(ctx, tx, who, plan, packet)
		}); err != nil {
			return zero, err
		}
		protection, err := p.Reader.prepareNativeTargetDefectProtection(ctx, who, plan, p.Configuration.Color)
		if err != nil {
			return zero, err
		}
		published, err := p.Reader.publishNativeTargetDefectProtection(ctx, who, plan, p.Configuration.Color, protection, p.Documents)
		if err != nil {
			return zero, err
		}
		if err := database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
			return p.Reader.registerNativeTargetDefectProtectionInTransaction(ctx, tx, who, plan, published, packet)
		}); err != nil {
			return zero, err
		}
		registered, err := p.Reader.readRegisteredNativeTargetDefectProtection(ctx, who, plan, p.Configuration.Color, published)
		if err != nil {
			return zero, err
		}
		if err := database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
			return p.Reader.lockNativeTargetDefectProtectionInTransaction(ctx, tx, who, plan, registered, packet)
		}); err != nil {
			return zero, err
		}
	}
	return nativeFacePipelineAdvance{phase: nativePreparationWaiting, operation: "semantic.transfer"}, ctx.Err()
}
