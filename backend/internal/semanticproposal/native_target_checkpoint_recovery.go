package semanticproposal

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Caller holds original parent lock. Checkpoint cannot replace live completed
// original protection, target producer evidence or final admission locks.
func (p TransferDraftPreparer) recoverNativeTargetCheckpoint(ctx context.Context, who auth.Identity, anchor nativePreparationAnchor, catalog v2draft.ArtifactCatalog, body []byte, sha string, catalogs NativeParentArtifactCatalogFactory) (nativeFacePipelineAdvance, error) {
	var zero nativeFacePipelineAdvance
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if p.Scheduler == nil || catalog == nil || catalogs == nil {
		return zero, v2input.ErrInvalid
	}
	manifest, err := restoreNativeTargetPreparationManifest(ctx, who, anchor.Request, anchor.Key, anchor.Published, anchor.Snapshot, anchor.SnapshotBody, body, sha)
	if err != nil {
		return zero, nativeTargetCheckpointStageError("restore_manifest", err)
	}
	producers, err := p.Reader.restoreNativeInputFaceProducers(ctx, catalog, who, anchor.Request, anchor.Key, anchor.Published, anchor.Snapshot, anchor.SnapshotBody, anchor.Color)
	if err != nil {
		return zero, nativeTargetCheckpointStageError("restore_face_producers", err)
	}
	presence, err := p.Reader.readNativeManualSelectionPresence(ctx, who, producers.detector)
	if err != nil {
		return zero, nativeTargetCheckpointStageError("read_manual_presence", err)
	}
	if presence != nativeStageAbsent {
		return zero, nativeTargetCheckpointStageError("manual_selection_present", v2input.ErrConflict)
	}
	locator, params, err := p.Reader.readNativeInputDefectSavedParams(ctx, who, anchor.Request, anchor.Published.Ref, anchor.Snapshot)
	if err != nil {
		return zero, nativeTargetCheckpointStageError("read_defect_params", err)
	}
	if locator.person == nil {
		return zero, nativeTargetCheckpointStageError("missing_person_locator", v2input.ErrConflict)
	}
	var values struct {
		Diameter [2]float64 `json:"diameter_face_fraction"`
	}
	if json.Unmarshal(params, &values) != nil || values.Diameter[0] <= 0 || values.Diameter[1] < values.Diameter[0] {
		return zero, nativeTargetCheckpointStageError("decode_defect_params", v2input.ErrInvalid)
	}
	defect, err := p.Reader.restoreNativeInputDefectDraftPlan(ctx, p.Scheduler, who, producers, anchor.Color, values.Diameter, nil, nil, nil, nil)
	if err != nil {
		return zero, nativeTargetCheckpointStageError("restore_defect_plan", err)
	}
	protection, err := p.Reader.restoreRegisteredNativeInputProtection(ctx, who, defect, anchor.Color)
	if err != nil {
		return zero, nativeTargetCheckpointStageError("restore_registered_protection", err)
	}
	d := producers.detector
	steps := []func(context.Context, auth.Identity, detectorDraftPlan, nativeTargetPreparationManifest, NativeParentArtifactCatalogFactory) (nativeFacePipelineAdvance, error){p.advanceNativeTargetPersons, p.advanceNativeTargetDetectors, p.advanceNativeTargetFaces, p.advanceNativeTargetSkins, p.advanceNativeTargetDefects, p.advanceNativeScenePrerequisites}
	for index, step := range steps {
		out, err := step(ctx, who, d, manifest, catalogs)
		if err != nil {
			return zero, nativeTargetCheckpointStageError(nativeTargetCheckpointStepLabels[index], err)
		}
		if out.phase != nativePreparationWaiting || out.operation != "semantic.transfer" {
			return out, nil
		}
	}
	ticket, err := p.admitPreparedNativeAllTargetParent(ctx, who, d, nativeInitialDefectAdvance{phase: nativePreparationSucceeded, protection: protection}, manifest, catalogs)
	if err != nil {
		return zero, nativeTargetCheckpointStageError("admit_parent", err)
	}
	if ticket.RunID != anchor.Request.RunID || ticket.WorkflowID != anchor.Request.WorkflowID || ticket.State != "queued" {
		return zero, nativeTargetCheckpointStageError("parent_ticket_identity", v2input.ErrConflict)
	}
	return nativeFacePipelineAdvance{phase: nativePreparationSucceeded, producers: producers}, nil
}
