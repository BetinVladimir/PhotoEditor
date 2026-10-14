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

type nativeTargetDefectSaved = nativeTargetDetectorSaved

func (s SQLTransferSourceReader) readNativeTargetDefectSaved(ctx context.Context, who auth.Identity, a nativeTargetAnchor) (nativeTargetDefectSaved, error) {
	var zero, out nativeTargetDefectSaved
	if err := verifyNativeTargetAnchor(ctx, a); err != nil {
		return zero, err
	}
	if s.DB == nil || a.stage.Operation != "defects.propose" {
		return zero, v2input.ErrInvalid
	}
	err := tenant.WithTenant(ctx, s.DB, who, a.request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var raw []byte
		if err := tx.QueryRow(ctx, "SELECT photo.semantic_target_defect_saved($1,$2,$3,$4,$5,$6,$7,$8)", a.request.TenantID, s.ProjectID, who.UserID, who.SessionID, a.request.RunID, a.entry.Ordinal, a.manifest.sha, a.stage.Operation).Scan(&raw); err != nil {
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
	run := uuid.NewSHA1(a.entry.Namespace, []byte("semantic.defect.prepare:reference"))
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
func (s SQLTransferSourceReader) recoverNativeTargetDefectTransport(ctx context.Context, tx pgx.Tx, who auth.Identity, original detectorDraftPlan, plan nativeTargetDefectDraftPlan) (v2input.Built, error) {
	var zero v2input.Built
	if err := verifyNativeTargetDefectPlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	a, err := compileNativeTargetAnchor(ctx, who, original.request, original.requestKey, original.published, original.anchor.snapshotRef, original.anchor.snapshotBody, plan.anchor.manifest, plan.anchor.entry.Ordinal, "defects.propose")
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
	if err := tx.QueryRow(ctx, "SELECT photo.semantic_target_defect_registered_transport($1,$2,$3,$4,$5)", plan.shell.Tenant, plan.shell.Run, task.ID, who.UserID, who.SessionID).Scan(&raw); err != nil {
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

// Restore only saved Params/report/costs/limits and target producers. No PUT,
// registration repair or queue mutation; current deployment limits are ignored.
func (s SQLTransferSourceReader) restoreNativeTargetDefectPlan(ctx context.Context, who auth.Identity, landmarks nativeTargetFaceDraftPlan, skin nativeTargetSkinDraftPlan, person nativeTargetPersonPlan, a nativeTargetAnchor, color TransferColorProfile, saved nativeTargetDefectSaved) (nativeTargetDefectDraftPlan, error) {
	var zero nativeTargetDefectDraftPlan
	if err := verifyNativeTargetAnchor(ctx, a); err != nil {
		return zero, err
	}
	if saved.State != "saved" || a.stage.Operation != "defects.propose" || a.entry.Namespace != landmarks.detector.anchor.entry.Namespace {
		return zero, v2input.ErrInvalid
	}
	var config struct {
		Stage struct {
			Recovery json.RawMessage `json:"recovery"`
			Diameter [2]float64      `json:"diameter_face_fraction"`
		} `json:"v27_target_defect_prepare"`
	}
	if json.Unmarshal(saved.Config, &config) != nil {
		return zero, v2input.ErrInvalid
	}
	m, err := decodeNativePlanRecoveryMetadata(config.Stage.Recovery, v2input.DraftTask{Operation: "defects.propose", ExecutionProfile: saved.Profile})
	if err != nil {
		return zero, err
	}
	if m.Report == nil || m.VRAM != 0 || m.RequestKey != "" || m.Continuation != "" || m.OrdinaryPolicy != nil || m.SkinProfileVersion != 0 || m.FaceProfileVersion != 0 || m.PersonProfileVersion != 0 {
		return zero, v2input.ErrConflict
	}
	prepared, err := s.prepareNativeTargetDefectParams(ctx, who, landmarks, skin, person, color, config.Stage.Diameter)
	if err != nil {
		return zero, err
	}
	qualification, err := s.prepareNativeTargetDefectQualification(ctx, who, prepared, color, *m.Report, m.Costs)
	if err != nil {
		return zero, err
	}
	published := PublishedDefectParams{Ref: saved.Params}
	if json.Unmarshal([]byte(prepared.params.inputs), &published.Inputs) != nil || json.Unmarshal([]byte(prepared.params.roots), &published.ManualRoots) != nil {
		return zero, v2input.ErrInvalid
	}
	task, err := s.prepareNativeTargetDefectTask(ctx, who, prepared, qualification, color, published, m.RAM, m.Scratch, m.Output)
	if err != nil {
		return zero, err
	}
	task.legacyQueueOmitted, err = nativePreparationLegacyQueue(string(saved.Config))
	if err != nil { return zero, err }
	plan, err := s.compileNativeTargetDefectDraftPlan(ctx, who, task, m.Reserved)
	if err != nil {
		return zero, err
	}
	if plan.anchor.seal != a.seal {
		return zero, v2input.ErrConflict
	}
	profile, err := canonical.Bytes(saved.Profile)
	if err != nil {
		return zero, err
	}
	if err := matchNativeInputSavedStagePlan(nativeInputSavedStage{run: saved.Run, task: saved.Task, params: saved.Params, configSHA: saved.ConfigSHA, inputDigest: saved.InputDigest, profile: string(profile), recovery: m}, plan.shell); err != nil {
		return zero, err
	}
	current, err := s.readNativeTargetDefectSaved(ctx, who, a)
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
	if err := s.recheckNativeTargetDefectQualification(ctx, who, prepared, qualification, color); err != nil {
		return zero, err
	}
	return plan, ctx.Err()
}
