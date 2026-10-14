package v2optical

import (
	"context"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"time"
)

// Admit first revalidates original report/arrays and exact registered transport.
// Graph activation and historical queued receipt commit atomically. Commit
// errors are ambiguous: callers must recover the stored receipt before retrying.
func (c PerspectiveCoordinator) Admit(ctx context.Context, who auth.Identity, request PerspectivePrepareRequest, prepared PreparedPerspective) error {
	if _, err := c.RegisterInputs(ctx, who, request, prepared); err != nil {
		return err
	}
	current, descriptor, err := c.revalidate(ctx, who, request, prepared)
	prepared = current
	if err != nil {
		return err
	}
	s, err := prepared.draft.Snapshot()
	if err != nil {
		return err
	}
	scope := artifactjson.Scope{Tenant: request.Tenant, Run: s.Run, Owner: s.Task, Purpose: "task_params"}
	repository := perspectiveParamsRepository{db: c.DB, bucket: c.Bucket, tenant: request.Tenant, who: who, plan: prepared.draft}
	proof, err := repository.proof(scope, s.ParamsSHA, int64(len(s.ParamsJCS)))
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
	var admitted bool
	if err = tx.QueryRow(ctx, "SELECT photo.seal_v29_perspective_graph($1,$2,$3,$4,$5::jsonb)", request.Tenant, s.Run, s.Task, string(descriptor), proof).Scan(&admitted); err != nil {
		return err
	}
	if !admitted {
		return v2input.ErrExpired
	}
	if err = LockLocatedPerspectiveQualification(ctx, tx, who, request.Tenant, request.Project, current.qualification); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
