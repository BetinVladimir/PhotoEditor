package v2optical

import (
	"context"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"time"
)

// Stage reserves output capacity and saves only a blocked task. It never
// publishes transport, registers inputs, seals a graph or queues execution.
func (c PerspectiveCoordinator) Stage(ctx context.Context, who auth.Identity, request PerspectivePrepareRequest, prepared PreparedPerspective) error {
	current, descriptor, err := c.revalidate(ctx, who, request, prepared)
	if err != nil {
		return err
	}
	snapshot, err := current.draft.Snapshot()
	if err != nil {
		return err
	}
	rebuilt, located := current.draft, current.qualification
	repository := perspectiveParamsRepository{db: c.DB, bucket: c.Bucket, tenant: request.Tenant, who: who, plan: rebuilt}
	scope := artifactjson.Scope{Tenant: request.Tenant, Run: snapshot.Run, Owner: snapshot.Task, Purpose: "task_params"}
	proof, err := repository.proof(scope, snapshot.ParamsSHA, int64(len(snapshot.ParamsJCS)))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := c.DB.Begin(ctx)
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
	if err = tx.QueryRow(ctx, "SELECT photo.stage_v29_perspective_task($1,$2,$3,$4::jsonb)", request.Tenant, snapshot.Run, string(descriptor), proof).Scan(&staged); err != nil {
		return err
	}
	if !staged {
		return v2input.ErrExpired
	}
	// Retain exact original qualification versions through the staging commit.
	if err = LockLocatedPerspectiveQualification(ctx, tx, who, request.Tenant, request.Project, located); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
