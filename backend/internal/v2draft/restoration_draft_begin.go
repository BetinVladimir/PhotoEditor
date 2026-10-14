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

const minRestorationDraftReservation int64 = 2162688

type GlareDonorDraftBeginRequest struct {
	Tenant            uuid.UUID
	Project           uuid.UUID
	Shoot             uuid.UUID
	Snapshot          uuid.UUID
	Actor             auth.Identity
	Key               string
	Ordinal           int
	Proposal          []byte
	Pin               v2portrait.RestorationQualificationPin
	SourceObservation uuid.UUID
	DonorObservation  uuid.UUID
	ReservedBytes     int64
}

type GlareDonorDraftIdentity struct {
	Run          uuid.UUID
	Workflow     uuid.UUID
	BaseVersion  uuid.UUID
	DonorVersion uuid.UUID
}

type SQLGlareDonorDraftBeginner struct {
	DB v2portrait.DonorConfirmationTransactions
}

// Begin persists a deterministic empty draft before the confirmation and
// Params documents exist. SQL repeats editor/snapshot/qualification/quota scope
// and permits replay only for the exact immutable configuration and reservation.
func (s SQLGlareDonorDraftBeginner) Begin(ctx context.Context,
	r GlareDonorDraftBeginRequest) (GlareDonorDraftIdentity, error) {
	var zero GlareDonorDraftIdentity
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || r.Tenant == uuid.Nil || r.Project == uuid.Nil || r.Shoot == uuid.Nil ||
		r.Snapshot == uuid.Nil || r.Actor.UserID == uuid.Nil || r.Actor.SessionID == uuid.Nil ||
		r.SourceObservation == uuid.Nil || r.DonorObservation == uuid.Nil ||
		r.SourceObservation == r.DonorObservation || r.Ordinal < 0 || r.Ordinal >= 5000 ||
		r.ReservedBytes < minRestorationDraftReservation || r.ReservedBytes > 9007199254740991 ||
		r.Pin.Operation != "portrait.glare" {
		return zero, v2input.ErrInvalid
	}
	if _, err := r.Pin.Compile(); err != nil {
		return zero, err
	}
	normal, err := canonical.Bytes(r.Proposal)
	if err != nil || !bytes.Equal(normal, r.Proposal) || len(normal) < 2 || len(normal) > 65536 {
		return zero, v2input.ErrInvalid
	}
	var proposal struct {
		Base struct {
			AssetVersionID uuid.UUID `json:"asset_version_id"`
		} `json:"base"`
		Donor struct {
			AssetVersionID uuid.UUID `json:"asset_version_id"`
		} `json:"donor"`
	}
	if json.Unmarshal(normal, &proposal) != nil || proposal.Base.AssetVersionID == uuid.Nil ||
		proposal.Donor.AssetVersionID == uuid.Nil ||
		proposal.Base.AssetVersionID == proposal.Donor.AssetVersionID {
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
	owner, err := v2input.TaskID(workflow, "portrait.glare", r.Ordinal)
	if err != nil {
		return zero, err
	}
	var proposedOwner struct {
		Confirmation v2input.Ref `json:"confirmation"`
	}
	if json.Unmarshal(normal, &proposedOwner) != nil ||
		proposedOwner.Confirmation.ArtifactID != owner {
		return zero, v2input.ErrInvalid
	}
	proposalSHA, err := canonical.Hash(json.RawMessage(normal))
	if err != nil {
		return zero, err
	}
	config, err := canonical.Marshal(map[string]any{
		"project_id": r.Project, "input_snapshot_id": r.Snapshot,
		"v28_restoration": map[string]any{
			"schema_version": 1, "tenant_id": r.Tenant, "project_id": r.Project,
			"snapshot_id": r.Snapshot, "actor_id": r.Actor.UserID,
			"auth_session_id": r.Actor.SessionID, "run_id": run, "workflow_id": workflow,
			"shoot_session_id": r.Shoot, "source_observation_id": r.SourceObservation,
			"donor_observation_id": r.DonorObservation, "operation": "portrait.glare",
			"ordinal": r.Ordinal, "params_proposal_sha": proposalSHA,
			"base_asset_version_id":  proposal.Base.AssetVersionID,
			"donor_asset_version_id": proposal.Donor.AssetVersionID,
			"qualification":          r.Pin,
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
	if err = tx.QueryRow(bounded, "SELECT photo.begin_v28_restoration_draft($1,$2,$3,$4)",
		string(config), string(normal), r.Pin.PolicySHA, r.ReservedBytes).Scan(&accepted); err != nil {
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
	return GlareDonorDraftIdentity{Run: run, Workflow: workflow,
		BaseVersion:  proposal.Base.AssetVersionID,
		DonorVersion: proposal.Donor.AssetVersionID}, nil
}
