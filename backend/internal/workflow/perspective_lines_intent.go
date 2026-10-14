package workflow

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type perspectiveLinesIntent struct {
	SchemaVersion int                           `json:"schema_version"`
	Tenant        uuid.UUID                     `json:"tenant_id"`
	Project       uuid.UUID                     `json:"project_id"`
	Run           uuid.UUID                     `json:"run_id"`
	Snapshot      uuid.UUID                     `json:"snapshot_id"`
	Actor         uuid.UUID                     `json:"actor_id"`
	Session       uuid.UUID                     `json:"auth_session_id"`
	Task          uuid.UUID                     `json:"task_id"`
	Base          v2input.SessionVersionBinding `json:"base"`
	WorkingICC    string                        `json:"working_icc_sha"`
	SourceJCS     string                        `json:"source_jcs"`
	ParamsJCS     string                        `json:"params_jcs"`
	ProfileJCS    string                        `json:"profile_jcs"`
	SeedJCS       string                        `json:"seed_jcs"`
}

func CompilePerspectiveLinesIntent(plan PerspectiveLinesPlan, snapshot uuid.UUID, who auth.Identity) (json.RawMessage, error) {
	if snapshot == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return nil, v2input.ErrInvalid
	}
	params := review.ArtifactRef{ID: uuid.NewSHA1(plan.task, []byte("validate-params")), SHA256: renderDerivativeBytesSHA(plan.params)}
	if _, err := PreparePerspectiveLinesTask(plan, params); err != nil {
		return nil, err
	}
	seed, err := canonical.Marshal(map[string]any{"tenant": plan.scope.Tenant, "project": plan.scope.Project, "params": json.RawMessage(plan.params), "working_icc_sha": plan.workingICC})
	if err != nil {
		return nil, err
	}
	return canonical.Marshal(perspectiveLinesIntent{SchemaVersion: 1, Tenant: plan.scope.Tenant, Project: plan.scope.Project, Run: plan.scope.Run, Snapshot: snapshot, Actor: who.UserID, Session: who.SessionID, Task: plan.task, Base: plan.scope.Base, WorkingICC: plan.workingICC, SourceJCS: string(plan.image), ParamsJCS: string(plan.params), ProfileJCS: string(plan.profile), SeedJCS: string(seed)})
}

type PerspectiveLinesIntentStore struct{ DB *pgxpool.Pool }

// Save persists only a source-authorized draft. No task/quota/queue is activated.
func (s PerspectiveLinesIntentStore) Save(ctx context.Context, plan PerspectiveLinesPlan, snapshot uuid.UUID, who auth.Identity) (uuid.UUID, error) {
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	raw, err := CompilePerspectiveLinesIntent(plan, snapshot, who)
	if err != nil {
		return uuid.Nil, err
	}
	var saved *uuid.UUID
	err = withPerspectivePreviewDocumentTx(ctx, s.DB, plan.scope.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT photo.save_v29_perspective_line_detection_intent($1,$2,$3,$4)", plan.scope.Tenant, who.UserID, who.SessionID, string(raw)).Scan(&saved)
	})
	if err != nil {
		return uuid.Nil, err
	}
	if saved == nil || *saved != plan.scope.Run {
		return uuid.Nil, v2input.ErrExpired
	}
	return *saved, nil
}
