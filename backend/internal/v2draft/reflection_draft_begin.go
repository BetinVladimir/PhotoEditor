package v2draft

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type ReflectionDraftBeginRequest struct {
	Tenant        uuid.UUID
	Project       uuid.UUID
	Actor         auth.Identity
	Key           string
	Intent        ReflectionClientIntent
	Pin           v2portrait.RestorationQualificationPin
	ReservedBytes int64
}

type ReflectionDraftIdentity struct {
	Run         uuid.UUID
	Workflow    uuid.UUID
	BaseVersion uuid.UUID
}

// Begin creates only a blocked, quota-reserved reflection draft. No Params or
// task is published; subsequent admission must replay live source authority.
func (s SQLGlareDonorDraftBeginner) BeginReflection(ctx context.Context,
	r ReflectionDraftBeginRequest) (ReflectionDraftIdentity, error) {
	var zero ReflectionDraftIdentity
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || r.Tenant == uuid.Nil || r.Project == uuid.Nil ||
		r.Actor.UserID == uuid.Nil || r.Actor.SessionID == uuid.Nil ||
		r.Pin.Operation != "image.reflection" ||
		r.ReservedBytes < minRestorationDraftReservation ||
		r.ReservedBytes > 9007199254740991 {
		return zero, v2input.ErrInvalid
	}
	intentRaw, err := canonical.Marshal(r.Intent)
	if err != nil {
		return zero, err
	}
	intent, err := DecodeReflectionClientIntent(intentRaw)
	if err != nil {
		return zero, err
	}
	if _, err := r.Pin.Compile(); err != nil {
		return zero, err
	}
	var proposal struct {
		Base struct {
			AssetVersionID uuid.UUID `json:"asset_version_id"`
		} `json:"base"`
		ModelLockSHA string      `json:"model_lock_sha"`
		Calibration  v2input.Ref `json:"calibration"`
	}
	if json.Unmarshal(intent.Proposal, &proposal) != nil ||
		proposal.ModelLockSHA != r.Pin.ModelLockSHA ||
		proposal.Calibration != r.Pin.Calibration {
		return zero, v2input.ErrInvalid
	}
	run, err := v2input.RunID(r.Tenant, r.Actor.UserID, r.Key)
	if err != nil {
		return zero, err
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return zero, err
	}
	if _, err := v2input.TaskID(workflow, "image.reflection", intent.Ordinal); err != nil {
		return zero, err
	}
	proposalSHA, err := canonical.Hash(json.RawMessage(intent.Proposal))
	if err != nil {
		return zero, err
	}
	config, err := canonical.Marshal(map[string]any{
		"project_id": r.Project, "input_snapshot_id": intent.Snapshot,
		"v28_restoration": map[string]any{
			"schema_version": 1, "tenant_id": r.Tenant, "project_id": r.Project,
			"snapshot_id": intent.Snapshot, "actor_id": r.Actor.UserID,
			"auth_session_id": r.Actor.SessionID, "run_id": run,
			"workflow_id": workflow, "operation": "image.reflection",
			"ordinal": intent.Ordinal, "params_proposal_sha": proposalSHA,
			"base_asset_version_id": proposal.Base.AssetVersionID,
			"target_pixel":          intent.TargetPixel, "protected_pixel": intent.ProtectedPixel,
			"qualification": r.Pin,
		},
	})
	if err != nil {
		return zero, err
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := s.DB.Begin(bounded)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	defer rollbackDonorRecovery(bounded, tx)
	if _, err = tx.Exec(bounded, "SELECT set_config('app.tenant_id',$1,true)", r.Tenant.String()); err != nil {
		return zero, err
	}
	var role string
	if err = tx.QueryRow(bounded, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	var accepted bool
	if err = tx.QueryRow(bounded, "SELECT photo.begin_v28_reflection_draft($1,$2,$3,$4)",
		string(config), string(intent.Proposal), r.Pin.PolicySHA,
		r.ReservedBytes).Scan(&accepted); err != nil {
		return zero, err
	}
	if !accepted {
		return zero, v2input.ErrExpired
	}
	if err = bounded.Err(); err != nil {
		return zero, err
	}
	if err = tx.Commit(bounded); err != nil {
		return zero, err
	}
	return ReflectionDraftIdentity{Run: run, Workflow: workflow,
		BaseVersion: proposal.Base.AssetVersionID}, nil
}
