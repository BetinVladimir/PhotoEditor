package v2optical

import (
	"context"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"time"
)

// ReadPublishedTransport restores committed documents without publication,
// quota repair, input registration or activation. Original report verification
// happens outside the transaction; SQL repeats current stored-plan authority.
func (c PerspectiveCoordinator) ReadPublishedTransport(ctx context.Context, who auth.Identity, request PerspectivePrepareRequest, prepared PreparedPerspective) (v2input.GraphTask, error) {
	var zero v2input.GraphTask
	current, descriptor, err := c.revalidate(ctx, who, request, prepared)
	if err != nil {
		return zero, err
	}
	s, err := current.draft.Snapshot()
	if err != nil {
		return zero, err
	}
	prepared = current
	repository := perspectiveParamsRepository{db: c.DB, bucket: c.Bucket, tenant: request.Tenant, who: who, plan: prepared.draft}
	scope := artifactjson.Scope{Tenant: request.Tenant, Run: s.Run, Owner: s.Task, Purpose: "task_params"}
	proof, err := repository.proof(scope, s.ParamsSHA, int64(len(s.ParamsJCS)))
	if err != nil {
		return zero, err
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	tx, err := c.DB.Begin(ctx)
	if err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = repository.authorize(ctx, tx, scope); err != nil {
		return zero, err
	}
	var raw []byte
	if err = tx.QueryRow(ctx, "SELECT photo.read_v29_perspective_published_transport($1,$2,$3,$4,$5::jsonb)", request.Tenant, s.Run, s.Task, string(descriptor), proof).Scan(&raw); err != nil {
		return zero, err
	}
	graph, err := prepared.RestorePublishedTransport(ctx, raw)
	if err != nil {
		return zero, err
	}
	if err = LockLocatedPerspectiveQualification(ctx, tx, who, request.Tenant, request.Project, current.qualification); err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return graph, nil
}
