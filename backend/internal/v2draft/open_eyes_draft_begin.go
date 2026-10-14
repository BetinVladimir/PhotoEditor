package v2draft

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type OpenEyesDraftBeginRequest struct {
	Tenant        uuid.UUID
	Project       uuid.UUID
	Actor         auth.Identity
	Key           string
	Intent        OpenEyesClientIntent
	Pin           v2portrait.DonorQualificationPin
	ReservedBytes int64
}

type OpenEyesDraftIdentity struct {
	Run      uuid.UUID
	Workflow uuid.UUID
	Owner    uuid.UUID
}

type SQLOpenEyesDraftBeginner struct {
	DB v2portrait.DonorConfirmationTransactions
}

// Begin persists an exact empty draft before issuing the confirmation object.
// SQL repeats current scope and returns false for changed replay or expired
// authorization. Object IO must happen only after this transaction commits.
func (s SQLOpenEyesDraftBeginner) Begin(ctx context.Context,
	r OpenEyesDraftBeginRequest) (OpenEyesDraftIdentity, error) {
	var zero OpenEyesDraftIdentity
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || r.Tenant == uuid.Nil || r.Project == uuid.Nil ||
		r.Actor.UserID == uuid.Nil || r.Actor.SessionID == uuid.Nil ||
		r.ReservedBytes < minRestorationDraftReservation || r.ReservedBytes > 9007199254740991 {
		return zero, v2input.ErrInvalid
	}
	if _, err := r.Pin.Compile(); err != nil {
		return zero, err
	}
	intentRaw, err := canonical.Marshal(r.Intent)
	if err != nil {
		return zero, err
	}
	if _, err = DecodeOpenEyesClientIntent(intentRaw); err != nil {
		return zero, err
	}
	run, workflow, err := VerifyOpenEyesIntentOwner(r.Intent, r.Tenant, r.Actor.UserID, r.Key)
	if err != nil {
		return zero, err
	}
	owner, err := v2input.TaskID(workflow, "portrait.open_eyes", r.Intent.Ordinal)
	if err != nil {
		return zero, err
	}
	proposal, _, err := r.Intent.Proposal.Compile()
	if err != nil || len(proposal) < 2 || len(proposal) > 65536 {
		return zero, v2input.ErrInvalid
	}
	proposalSHA, err := canonical.Hash(json.RawMessage(proposal))
	if err != nil {
		return zero, err
	}
	intentSHA, err := canonical.Hash(r.Intent)
	if err != nil {
		return zero, err
	}
	config, err := canonical.Marshal(map[string]any{
		"project_id": r.Project, "input_snapshot_id": r.Intent.Snapshot,
		"v28_open_eyes": map[string]any{
			"schema_version": 1, "tenant_id": r.Tenant, "project_id": r.Project,
			"snapshot_id": r.Intent.Snapshot, "actor_id": r.Actor.UserID,
			"auth_session_id": r.Actor.SessionID, "run_id": run,
			"workflow_id": workflow, "shoot_session_id": r.Intent.Shoot,
			"source_observation_id": r.Intent.SourceObservation,
			"donor_observation_id":  r.Intent.DonorObservation,
			"operation":             "portrait.open_eyes", "ordinal": r.Intent.Ordinal,
			"params_proposal_sha":    proposalSHA,
			"client_intent_sha":      intentSHA,
			"base_asset_version_id":  r.Intent.Proposal.Base.AssetVersionID,
			"donor_asset_version_id": r.Intent.Proposal.Donor.AssetVersionID,
			"qualification":          r.Pin,
		},
	})
	if err != nil || len(config) < 2 || len(config) > 1048576 {
		return zero, v2input.ErrInvalid
	}
	// Keep the exact typed wire body across the SQL boundary.
	normal, err := canonical.Bytes(proposal)
	if err != nil || !bytes.Equal(normal, proposal) {
		return zero, v2input.ErrInvalid
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
	if err = tx.QueryRow(bounded, "SELECT photo.begin_v28_open_eyes_draft($1,$2,$3,$4)",
		string(config), string(proposal), r.Pin.PolicySHA, r.ReservedBytes).Scan(&accepted); err != nil {
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
	return OpenEyesDraftIdentity{Run: run, Workflow: workflow, Owner: owner}, nil
}
