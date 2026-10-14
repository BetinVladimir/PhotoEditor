package v2portrait

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"time"
)

// SaveRelightAnalysisDraft persists only the immutable preliminary plan. Live
// report bytes, source arrays, Params publication and admission remain separate.
func SaveRelightAnalysisDraft(ctx context.Context, db DonorConfirmationTransactions, who auth.Identity, tenant uuid.UUID, plan RelightAnalysisPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if db == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenant == uuid.Nil {
		return v2input.ErrInvalid
	}
	snapshot := plan.Snapshot()
	var scope struct {
		Tenant  uuid.UUID `json:"tenant_id"`
		Project uuid.UUID `json:"project_id"`
		Actor   uuid.UUID `json:"actor_id"`
		Session uuid.UUID `json:"auth_session_id"`
	}
	if snapshot.Run == uuid.Nil || snapshot.Workflow == uuid.Nil || snapshot.Task == uuid.Nil || snapshot.ParamsDocument == uuid.Nil || len(snapshot.Config) < 2 || json.Unmarshal(snapshot.Config, &scope) != nil || scope.Tenant != tenant || scope.Project == uuid.Nil || scope.Actor != who.UserID || scope.Session != who.SessionID {
		return v2input.ErrInvalid
	}
	body, err := canonical.Marshal(snapshot)
	if err != nil {
		return err
	}
	config, err := canonical.Marshal(map[string]any{"project_id": scope.Project, "v28_analysis_plan": json.RawMessage(body)})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	defer tx.Rollback(context.Background())
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return err
	}
	var saved bool
	if err = tx.QueryRow(ctx, "SELECT photo.save_v28_relight_analysis_draft($1,$2,$3,$4,$5)", tenant, who.UserID, who.SessionID, string(body), string(config)).Scan(&saved); err != nil {
		return err
	}
	if !saved {
		return v2input.ErrExpired
	}
	return tx.Commit(ctx)
}
