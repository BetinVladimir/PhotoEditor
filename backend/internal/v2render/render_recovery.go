package v2render

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

type RecoveredRenderDraft struct {
	RunID, WorkflowID, ProjectID, SnapshotID uuid.UUID
	Config                                   json.RawMessage
	ConfigSHA, PolicySHA                     string
	ReservedBytes                            int64
}

// Load retrieves immutable draft evidence for reconciliation. It neither
// requalifies providers nor recreates inventories, publishes or activates tasks.
func (s SQLRenderDraftStore) Load(ctx context.Context, tenant, run uuid.UUID, who auth.Identity) (RecoveredRenderDraft, error) {
	if err := ctx.Err(); err != nil {
		return RecoveredRenderDraft{}, err
	}
	if s.DB == nil || tenant == uuid.Nil || run == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return RecoveredRenderDraft{}, v2input.ErrInvalid
	}
	result := RecoveredRenderDraft{RunID: run}
	err := database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		return tx.QueryRow(ctx, `SELECT workflow_id,project_id,snapshot_id,config,config_sha,policy_sha,reserved_bytes FROM photo.v26_render_draft_state($1,$2,$3,$4)`, tenant, run, who.UserID, who.SessionID).Scan(&result.WorkflowID, &result.ProjectID, &result.SnapshotID, &result.Config, &result.ConfigSHA, &result.PolicySHA, &result.ReservedBytes)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return RecoveredRenderDraft{}, v2input.ErrExpired
	}
	if err != nil {
		return RecoveredRenderDraft{}, err
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil || workflow != result.WorkflowID || result.ProjectID == uuid.Nil || result.SnapshotID == uuid.Nil || result.ReservedBytes < 1 || result.ReservedBytes > 9007199254740991 {
		return RecoveredRenderDraft{}, v2input.ErrInvalid
	}
	body, err := canonical.Bytes(result.Config)
	if err != nil || len(body) > 1048576 {
		return RecoveredRenderDraft{}, v2input.ErrInvalid
	}
	digest, err := canonical.Hash(json.RawMessage(body))
	if err != nil || digest != result.ConfigSHA {
		return RecoveredRenderDraft{}, v2input.ErrInvalid
	}
	result.Config = body
	if _, err := DecodeRecoveredRenderPlan(result); err != nil {
		return RecoveredRenderDraft{}, err
	}
	return result, nil
}
