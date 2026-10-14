package v2portrait

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
func (c RelightAnalysisCoordinator) Admit(ctx context.Context, who auth.Identity, request RelightAnalysisPlanRequest, prepared PreparedRelightAnalysis) error {
	if _, err := c.RegisterInputs(ctx, who, request, prepared); err != nil {
		return err
	}
	descriptor, err := prepared.plan.Descriptor(ctx)
	if err != nil {
		return err
	}
	s := prepared.plan.Snapshot()
	scope := artifactjson.Scope{Tenant: request.Tenant, Run: s.Run, Owner: s.Task, Purpose: "task_params"}
	repository := relightAnalysisParamsRepository{db: c.DB, bucket: c.Bucket, tenant: request.Tenant, who: who, plan: prepared.plan}
	proof, err := repository.proof(scope, s.ParamsSHA, int64(len(s.Params)))
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
	if err = tx.QueryRow(ctx, "SELECT photo.seal_v28_relight_analysis_graph($1,$2,$3,$4,$5::jsonb)", request.Tenant, s.Run, s.Task, string(descriptor), proof).Scan(&admitted); err != nil {
		return err
	}
	if !admitted {
		return v2input.ErrExpired
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
