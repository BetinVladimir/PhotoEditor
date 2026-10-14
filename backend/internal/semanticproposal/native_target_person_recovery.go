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

type nativeTargetPersonSaved struct {
	State       string          `json:"state"`
	Run         uuid.UUID       `json:"run_id"`
	Task        uuid.UUID       `json:"task_id"`
	Config      json.RawMessage `json:"config"`
	ConfigSHA   string          `json:"config_sha"`
	InputDigest string          `json:"input_digest"`
	Profile     json.RawMessage `json:"profile"`
	Params      v2input.Ref     `json:"params"`
	RunState    string          `json:"run_state"`
	TaskState   string          `json:"task_state"`
	Sealed      bool            `json:"sealed"`
}

func (s SQLTransferSourceReader) readNativeTargetPersonSaved(ctx context.Context, who auth.Identity, a nativeTargetAnchor) (nativeTargetPersonSaved, error) {
	var zero, out nativeTargetPersonSaved
	if err := verifyNativeTargetAnchor(ctx, a); err != nil {
		return zero, err
	}
	if s.DB == nil || a.stage.Operation != "mask.person" {
		return zero, v2input.ErrInvalid
	}
	err := tenant.WithTenant(ctx, s.DB, who, a.request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var raw []byte
		if err := tx.QueryRow(ctx, "SELECT photo.semantic_target_person_saved($1,$2,$3,$4,$5,$6,$7)", a.request.TenantID, s.ProjectID, who.UserID, who.SessionID, a.request.RunID, a.entry.Ordinal, a.manifest.sha).Scan(&raw); err != nil {
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
	run, workflow, err := personPreparationIDs(a.entry.Namespace)
	if err != nil {
		return zero, err
	}
	task, err := v2input.TaskID(workflow, "mask.person", 0)
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

// Reconstruct from saved Params/profile/limits and re-read physical target bytes.
// Does not publish replacement Params or silently apply current capacity limits.
func (s SQLTransferSourceReader) restoreNativeTargetPersonPlan(ctx context.Context, catalog v2draft.ArtifactCatalog, who auth.Identity, a nativeTargetAnchor, color TransferColorProfile, saved nativeTargetPersonSaved) (nativeTargetPersonPlan, error) {
	var zero nativeTargetPersonPlan
	if err := verifyNativeTargetAnchor(ctx, a); err != nil {
		return zero, err
	}
	if saved.State != "saved" || catalog == nil {
		return zero, v2input.ErrInvalid
	}
	var config struct {
		Stage struct {
			Recovery json.RawMessage `json:"recovery"`
		} `json:"v27_target_person_prepare"`
	}
	if json.Unmarshal(saved.Config, &config) != nil {
		return zero, v2input.ErrInvalid
	}
	metadata, err := decodeNativePlanRecoveryMetadata(config.Stage.Recovery, v2input.DraftTask{Operation: "mask.person", ExecutionProfile: saved.Profile})
	if err != nil {
		return zero, err
	}
	if metadata.Report == nil || metadata.RequestKey != a.key || metadata.Continuation != "" || metadata.OrdinaryPolicy != nil {
		return zero, v2input.ErrConflict
	}
	document := uuid.NewSHA1(a.entry.Namespace, []byte("semantic.mask.person.params:reference"))
	object, err := s.defectVersionParamsObject(ctx, who, a.request.TenantID, a.entry.Binding, document, saved.Params)
	if err != nil {
		return zero, err
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, object, saved.Params)
	if err != nil {
		return zero, err
	}
	var params struct {
		Tile int `json:"guided_tile"`
	}
	if json.Unmarshal(raw, &params) != nil {
		return zero, v2input.ErrInvalid
	}
	policy, err := s.readQualificationPolicySnapshot(ctx, who, a.request.TenantID)
	if err != nil {
		return zero, err
	}
	selected, err := selectQualificationPolicies(policy, []string{"mask.person"})
	if err != nil {
		return zero, err
	}
	prepared, err := s.prepareSourcePersonTask(ctx, catalog, who, a.request.TenantID, a.entry.Namespace, a.entry.Binding, color, document, saved.Params, *metadata.Report, selected["mask.person"], params.Tile, metadata.Costs, metadata.RAM, metadata.VRAM, metadata.Scratch, metadata.Output)
	if err != nil {
		return zero, err
	}
	prepared.legacyQueueOmitted, err = nativePreparationLegacyQueue(string(saved.Config))
	if err != nil { return zero, err }
	plan, err := s.compileNativeTargetPersonPlan(ctx, who, a, prepared, policy, metadata.Reserved)
	if err != nil {
		return zero, err
	}
	profile, err := canonical.Bytes(saved.Profile)
	if err != nil {
		return zero, err
	}
	if err := matchNativeInputSavedStagePlan(nativeInputSavedStage{run: saved.Run, task: saved.Task, params: saved.Params, configSHA: saved.ConfigSHA, inputDigest: saved.InputDigest, profile: string(profile), recovery: metadata}, plan.person.shell); err != nil {
		return zero, err
	}
	currentObject, err := s.defectVersionParamsObject(ctx, who, a.request.TenantID, a.entry.Binding, document, saved.Params)
	if err != nil {
		return zero, err
	}
	if currentObject != object {
		return zero, v2input.ErrConflict
	}
	current, err := s.readNativeTargetPersonSaved(ctx, who, a)
	if err != nil {
		return zero, err
	}
	if current.ConfigSHA != saved.ConfigSHA || current.Task != saved.Task || current.Params != saved.Params || current.InputDigest != saved.InputDigest {
		return zero, v2input.ErrConflict
	}
	beforeProfile, err := canonical.Hash(saved.Profile)
	if err != nil {
		return zero, err
	}
	afterProfile, err := canonical.Hash(current.Profile)
	if err != nil || beforeProfile != afterProfile {
		return zero, v2input.ErrConflict
	}

	if err := s.recheckNativePlanPolicy(ctx, who, a.request.TenantID, policy); err != nil {
		return zero, err
	}
	return plan, ctx.Err()
}
func (s SQLTransferSourceReader) recoverNativeTargetPersonTransport(ctx context.Context, tx pgx.Tx, who auth.Identity, d detectorDraftPlan, plan nativeTargetPersonPlan) (v2input.Built, error) {
	var zero v2input.Built
	if err := lockNativeTargetAnchorInTransaction(ctx, tx, who, d, plan.anchor); err != nil {
		return zero, err
	}
	task, err := plan.person.prepared.catalog.frozen.DraftTask()
	if err != nil {
		return zero, err
	}
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT photo.semantic_target_person_registered_transport($1,$2,$3,$4,$5)", plan.anchor.request.TenantID, plan.person.shell.Run, task.ID, who.UserID, who.SessionID).Scan(&raw); err != nil {
		return zero, err
	}
	built, err := decodeNativeRecoveredTransport(raw, task.ID)
	if err != nil {
		return zero, err
	}
	if _, err := plan.person.prepared.catalog.frozen.BindPublishedTransport(ctx, built); err != nil {
		return zero, err
	}
	return built, ctx.Err()
}
