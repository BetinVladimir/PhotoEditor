package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

type SQLRenderDraftStore struct{ DB *pgxpool.Pool }

// Begin reserves quota and atomically stages every blocked render task. It does
// not perform object I/O, register input manifests or activate the graph.
func (s SQLRenderDraftStore) Begin(ctx context.Context, tenant, run uuid.UUID, who auth.Identity, policySHA string, plan CompiledRenderRun) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || tenant == uuid.Nil || run == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || len(policySHA) != 64 || len(plan.Config) < 2 || len(plan.Config) > 1048576 || plan.OutputBytes < 1 {
		return v2input.ErrInvalid
	}
	for _, c := range policySHA {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return v2input.ErrInvalid
		}
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil || workflow != plan.Workflow {
		return v2input.ErrInvalid
	}
	body, err := canonical.Bytes(plan.Config)
	if err != nil || !bytes.Equal(body, plan.Config) {
		return v2input.ErrInvalid
	}
	digest, err := canonical.Hash(json.RawMessage(body))
	if err != nil || digest != plan.ConfigSHA {
		return v2input.ErrInvalid
	}
	var config struct {
		Project  uuid.UUID `json:"project_id"`
		Snapshot uuid.UUID `json:"input_snapshot_id"`
		Render   struct {
			Session uuid.UUID `json:"auth_session_id"`
		} `json:"v26_render"`
	}
	if json.Unmarshal(body, &config) != nil || config.Project == uuid.Nil || config.Snapshot == uuid.Nil || config.Render.Session != who.SessionID {
		return v2input.ErrInvalid
	}
	return database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.begin_v26_render_draft($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, tenant, config.Project, config.Snapshot, who.UserID, who.SessionID, run, workflow, string(body), policySHA, plan.OutputBytes).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return v2input.ErrExpired
		}
		return nil
	})
}
