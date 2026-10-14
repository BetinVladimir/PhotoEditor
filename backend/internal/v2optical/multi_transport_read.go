package v2optical

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"time"
)

// ReadMultiPublishedTransport restores committed documents without publication,
// quota repair, input registration or activation. Original report verification
// happens outside the transaction; SQL repeats current stored-plan authority.
func ReadMultiPublishedTransport(ctx context.Context, db PerspectiveTransactions, bucket string, who auth.Identity, tenant uuid.UUID, draft MultiDraft, qualified VerifiedMultiQualification, params v2input.Ref) (v2input.GraphTask, error) {
	var zero v2input.GraphTask
	descriptor, err := draft.Descriptor(ctx, qualified, params)
	if err != nil {
		return zero, err
	}
	s, err := draft.Snapshot()
	if err != nil {
		return zero, err
	}

	repository := multiParamsRepository{db: db, bucket: bucket, tenant: tenant, who: who, plan: draft}
	scope := artifactjson.Scope{Tenant: tenant, Run: s.Run, Owner: s.Task, Purpose: "task_params"}
	proof, err := repository.proof(scope, s.ParamsSHA, int64(len(s.ParamsJCS)))
	if err != nil {
		return zero, err
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	tx, err := db.Begin(ctx)
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
	if err = tx.QueryRow(ctx, "SELECT photo.read_v29_multi_published_transport($1,$2,$3,$4,$5::jsonb)", tenant, s.Run, s.Task, string(descriptor), proof).Scan(&raw); err != nil {
		return zero, err
	}
	graph, err := RestoreMultiTransport(ctx, draft, qualified, params, raw)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return graph, nil
}
