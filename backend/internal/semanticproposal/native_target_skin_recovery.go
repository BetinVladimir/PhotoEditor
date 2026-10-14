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
	"photoeditor/backend/internal/v2input"
)

type nativeTargetSkinSaved = nativeTargetDetectorSaved

func (s SQLTransferSourceReader) readNativeTargetSkinSaved(ctx context.Context, who auth.Identity, a nativeTargetAnchor) (nativeTargetSkinSaved, error) {
	var zero, out nativeTargetSkinSaved
	if err := verifyNativeTargetAnchor(ctx, a); err != nil {
		return zero, err
	}
	if s.DB == nil || a.stage.Operation != "mask.face_skin" {
		return zero, v2input.ErrInvalid
	}
	err := tenant.WithTenant(ctx, s.DB, who, a.request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var raw []byte
		if err := tx.QueryRow(ctx, "SELECT photo.semantic_target_skin_saved($1,$2,$3,$4,$5,$6,$7,$8)", a.request.TenantID, s.ProjectID, who.UserID, who.SessionID, a.request.RunID, a.entry.Ordinal, a.manifest.sha, a.stage.Operation).Scan(&raw); err != nil {
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
	run := uuid.NewSHA1(a.entry.Namespace, []byte("semantic.mask.face_skin:reference"))
	workflow, err := v2input.WorkflowID(run)
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

// Caller holds original parent lock. Decode and bind only the exact persisted
// transport; this method performs no object IO, registration repair or queueing.
func (s SQLTransferSourceReader) recoverNativeTargetSkinTransport(ctx context.Context, tx pgx.Tx, who auth.Identity, original detectorDraftPlan, plan nativeTargetSkinDraftPlan) (v2input.Built, error) {
	var zero v2input.Built
	if err := verifyNativeTargetSkinPlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	a, err := compileNativeTargetAnchor(ctx, who, original.request, original.requestKey, original.published, original.anchor.snapshotRef, original.anchor.snapshotBody, plan.parser.detector.anchor.manifest, plan.parser.detector.anchor.entry.Ordinal, "mask.face_skin")
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
	if err := tx.QueryRow(ctx, "SELECT photo.semantic_target_skin_registered_transport($1,$2,$3,$4,$5)", plan.shell.Tenant, plan.shell.Run, task.ID, who.UserID, who.SessionID).Scan(&raw); err != nil {
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

// Restore only exact saved limits/profile and immutable Params; never publish
// replacement objects or substitute current capacity settings.
func (s SQLTransferSourceReader) restoreNativeTargetSkinPlan(ctx context.Context, who auth.Identity, parser nativeTargetFaceDraftPlan, a nativeTargetAnchor, color TransferColorProfile, saved nativeTargetSkinSaved) (nativeTargetSkinDraftPlan, error) {
	var zero nativeTargetSkinDraftPlan
	if err := verifyNativeTargetAnchor(ctx, a); err != nil {
		return zero, err
	}
	if err := verifyNativeTargetFacePlanSeal(ctx, parser); err != nil {
		return zero, err
	}
	if saved.State != "saved" || a.stage.Operation != "mask.face_skin" || parser.prepared.operation != "face.parse" || a.entry.Namespace != parser.detector.anchor.entry.Namespace {
		return zero, v2input.ErrInvalid
	}
	var config struct {
		Stage struct {
			Recovery json.RawMessage `json:"recovery"`
		} `json:"v27_target_skin_prepare"`
	}
	if json.Unmarshal(saved.Config, &config) != nil {
		return zero, v2input.ErrInvalid
	}
	m, err := decodeNativePlanRecoveryMetadata(config.Stage.Recovery, v2input.DraftTask{Operation: "mask.face_skin", ExecutionProfile: saved.Profile})
	if err != nil {
		return zero, err
	}
	if m.Report != nil || m.RequestKey != "" || m.Continuation != "" || m.OrdinaryPolicy != nil {
		return zero, v2input.ErrConflict
	}
	document := uuid.NewSHA1(a.entry.Namespace, []byte("semantic.mask.face_skin.params:reference"))
	object, err := s.defectVersionParamsObject(ctx, who, a.request.TenantID, a.entry.Binding, document, saved.Params)
	if err != nil {
		return zero, err
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, object, saved.Params)
	if err != nil {
		return zero, err
	}
	result, err := s.readNativeTargetFaceResult(ctx, who, parser, color)
	if err != nil {
		return zero, err
	}
	params := nativeTargetSkinParams{document: document, ref: saved.Params, parser: result}
	prepared, err := s.prepareNativeTargetSkinTask(ctx, who, parser, color, params, m.RAM, m.Scratch, m.Output, m.SkinProfileVersion)
	if err != nil {
		return zero, err
	}
	prepared.legacyQueueOmitted, err = nativePreparationLegacyQueue(string(saved.Config))
	if err != nil { return zero, err }
	plan, err := s.compileNativeTargetSkinDraftPlan(ctx, who, parser, prepared, m.Reserved)
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
	if string(raw) != string(currentRaw) {
		return zero, v2input.ErrConflict
	}
	current, err := s.readNativeTargetSkinSaved(ctx, who, a)
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
	if err := s.recheckNativePlanPolicy(ctx, who, a.request.TenantID, parser.detector.detector.policy); err != nil {
		return zero, err
	}
	return plan, ctx.Err()
}
