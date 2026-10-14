package v2optical

import (
	"context"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// StageMultiTask persists a blocked shell and reserves the exact compiled output
// budget. SQL repeats live tenant/session/source/qualification/resource authority;
// this function does not publish transport, seal inputs or queue execution.
func StageMultiTask(ctx context.Context, db PerspectiveTransactions, bucket string, who auth.Identity,
	tenant uuid.UUID, draft MultiDraft, qualified VerifiedMultiQualification, params v2input.Ref) error {
	descriptor, err := draft.Descriptor(ctx, qualified, params)
	if err != nil {
		return err
	}
	snapshot, err := draft.Snapshot()
	if err != nil {
		return err
	}
	repository := multiParamsRepository{db: db, bucket: bucket, tenant: tenant, who: who, plan: draft}
	scope := artifactjson.Scope{Tenant: tenant, Run: snapshot.Run, Owner: snapshot.Task, Purpose: "task_params"}
	proof, err := repository.proof(scope, snapshot.ParamsSHA, int64(len(snapshot.ParamsJCS)))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = repository.authorize(ctx, tx, scope); err != nil {
		return err
	}
	var staged bool
	if err = tx.QueryRow(ctx, "SELECT photo.stage_v29_multi_task($1,$2,$3,$4::jsonb)", tenant, snapshot.Run, string(descriptor), proof).Scan(&staged); err != nil {
		return err
	}
	if !staged {
		return v2input.ErrExpired
	}
	return tx.Commit(ctx)
}
