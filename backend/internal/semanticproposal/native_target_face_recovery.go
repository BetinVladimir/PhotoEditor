package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type nativeTargetFaceSaved = nativeTargetDetectorSaved

func (s SQLTransferSourceReader) readNativeTargetFaceSaved(ctx context.Context, who auth.Identity, a nativeTargetAnchor) (nativeTargetFaceSaved, error) {
	var zero, out nativeTargetFaceSaved
	if err := verifyNativeTargetAnchor(ctx, a); err != nil {
		return zero, err
	}
	if s.DB == nil || (a.stage.Operation != "face.landmarks" && a.stage.Operation != "face.parse") {
		return zero, v2input.ErrInvalid
	}
	err := tenant.WithTenant(ctx, s.DB, who, a.request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var raw []byte
		if err := tx.QueryRow(ctx, "SELECT photo.semantic_target_face_saved($1,$2,$3,$4,$5,$6,$7,$8)", a.request.TenantID, s.ProjectID, who.UserID, who.SessionID, a.request.RunID, a.entry.Ordinal, a.manifest.sha, a.stage.Operation).Scan(&raw); err != nil {
			return err
		}
		if len(raw) == 0 || len(raw) > 5<<20 || json.Unmarshal(raw, &out) != nil {
			return v2input.ErrConflict
		}
		return nil
	})
	if err != nil {
		return zero, err
	}
	if out.State == "absent" {
		return out, ctx.Err()
	}
	run, workflow, err := nativeFacePreparationIDs(a.entry.Namespace, a.stage.Operation)
	if err != nil {
		return zero, err
	}
	task, err := v2input.TaskID(workflow, a.stage.Operation, 0)
	if err != nil {
		return zero, err
	}
	sha, err := canonical.Hash(out.Config)
	if err != nil || sha != out.ConfigSHA || out.State != "saved" || out.Run != run || out.Task != task {
		return zero, v2input.ErrConflict
	}
	if _, err := nativeInputStagePreparationPhase(nativeInputSavedStageState{task: out.Task, taskState: out.TaskState, runState: out.RunState, sealed: out.Sealed}); err != nil {
		return zero, err
	}
	return out, ctx.Err()
}

// Restore exact saved capacity/report/profile and immutable Params, never publish
// replacements or substitute current capacity configuration for saved limits.
func (s SQLTransferSourceReader) restoreNativeTargetFacePlan(ctx context.Context, catalog v2draft.ArtifactCatalog, who auth.Identity, detector nativeTargetDetectorPlan, a nativeTargetAnchor, color TransferColorProfile, saved nativeTargetFaceSaved) (nativeTargetFaceDraftPlan, error) {
	var zero nativeTargetFaceDraftPlan
	if err := verifyNativeTargetAnchor(ctx, a); err != nil {
		return zero, err
	}
	if err := verifyNativeTargetDetectorPlan(ctx, detector); err != nil {
		return zero, err
	}
	if saved.State != "saved" || catalog == nil || a.entry.Namespace != detector.anchor.entry.Namespace {
		return zero, v2input.ErrInvalid
	}
	var config struct {
		Stage struct {
			Recovery json.RawMessage `json:"recovery"`
		} `json:"v27_target_face_prepare"`
	}
	if json.Unmarshal(saved.Config, &config) != nil {
		return zero, v2input.ErrInvalid
	}
	m, err := decodeNativePlanRecoveryMetadata(config.Stage.Recovery, v2input.DraftTask{Operation: a.stage.Operation, ExecutionProfile: saved.Profile})
	if err != nil {
		return zero, err
	}
	if m.Report == nil || m.RequestKey != a.key || m.Continuation != "" || m.OrdinaryPolicy != nil {
		return zero, v2input.ErrConflict
	}
	document := uuid.NewSHA1(a.entry.Namespace, []byte("semantic."+a.stage.Operation+".params:reference"))
	object, err := s.defectVersionParamsObject(ctx, who, a.request.TenantID, a.entry.Binding, document, saved.Params)
	if err != nil {
		return zero, err
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, object, saved.Params)
	if err != nil {
		return zero, err
	}
	result, err := s.readNativeTargetDetectorResult(ctx, who, detector, color)
	if err != nil {
		return zero, err
	}
	c := result.completed
	params := nativeFaceStageParams{operation: a.stage.Operation, document: document, ref: saved.Params, detector: c.catalog, sourceBody: c.sourceBody, facesBody: c.body, detectorParamsBody: c.detectorParamsBody}
	budget := nativePreparationBudget{Costs: m.Costs, RAM: m.RAM, VRAM: m.VRAM, Scratch: m.Scratch, Output: m.Output, Reserved: m.Reserved}
	prepared, err := s.prepareNativeTargetFaceTask(ctx, catalog, who, detector, color, params, *m.Report, budget, m.FaceProfileVersion)
	if err != nil {
		return zero, err
	}
	prepared.legacyQueueOmitted, err = nativePreparationLegacyQueue(string(saved.Config))
	if err != nil { return zero, err }
	plan, err := s.compileNativeTargetFaceDraftPlan(ctx, who, detector, result, prepared, m.Reserved)
	if err != nil {
		return zero, err
	}
	profile, err := canonical.Bytes(saved.Profile)
	if err != nil {
		return zero, err
	}
	if err := matchNativeInputSavedStagePlan(nativeInputSavedStage{run: saved.Run, task: saved.Task, params: saved.Params, configSHA: saved.ConfigSHA, inputDigest: saved.InputDigest, profile: string(profile), recovery: m}, plan.shell); err != nil {
		return zero, err
	}
	after, err := s.defectVersionParamsObject(ctx, who, a.request.TenantID, a.entry.Binding, document, saved.Params)
	if err != nil {
		return zero, err
	}
	if after != object {
		return zero, v2input.ErrConflict
	}
	currentRaw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, after, saved.Params)
	if err != nil {
		return zero, err
	}
	if string(currentRaw) != string(raw) {
		return zero, v2input.ErrConflict
	}
	current, err := s.readNativeTargetFaceSaved(ctx, who, a)
	if err != nil {
		return zero, err
	}
	beforeSHA, err := canonical.Hash(saved.Profile)
	if err != nil {
		return zero, err
	}
	afterSHA, err := canonical.Hash(current.Profile)
	if err != nil || beforeSHA != afterSHA || current.ConfigSHA != saved.ConfigSHA || current.Task != saved.Task || current.Params != saved.Params || current.InputDigest != saved.InputDigest {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckNativePlanPolicy(ctx, who, a.request.TenantID, detector.detector.policy); err != nil {
		return zero, err
	}
	return plan, ctx.Err()
}

// Caller holds original parent lock. Decode and bind only the exact persisted
// transport; this method performs no object IO, registration repair or queueing.
func (s SQLTransferSourceReader) recoverNativeTargetFaceTransport(ctx context.Context, tx pgx.Tx, who auth.Identity, original detectorDraftPlan, plan nativeTargetFaceDraftPlan) (v2input.Built, error) {
	var zero v2input.Built
	if err := verifyNativeTargetFacePlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	a, err := compileNativeTargetAnchor(ctx, who, original.request, original.requestKey, original.published, original.anchor.snapshotRef, original.anchor.snapshotBody, plan.detector.anchor.manifest, plan.detector.anchor.entry.Ordinal, plan.prepared.operation)
	if err != nil {
		return zero, err
	}
	if err := lockNativeTargetAnchorInTransaction(ctx, tx, who, original, a); err != nil {
		return zero, err
	}
	task, err := plan.prepared.frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT photo.semantic_target_face_registered_transport($1,$2,$3,$4,$5)", plan.shell.Tenant, plan.shell.Run, task.ID, who.UserID, who.SessionID).Scan(&raw); err != nil {
		return zero, err
	}
	built, err := decodeNativeRecoveredTransport(raw, task.ID)
	if err != nil {
		return zero, err
	}
	if _, err := plan.prepared.frozen.BindPublishedTransport(ctx, built); err != nil {
		return zero, err
	}
	return built, ctx.Err()
}
