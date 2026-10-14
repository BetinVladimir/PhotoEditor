package v2portrait

import (
	"bytes"
	"context"
	"time"

	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Stage persists a blocked producer and reserves outputs. It re-reads original
// qualification evidence before the transaction; the SQL scope repeats current
// policy/source authorization. This method never seals or queues the task.
func (c RelightAnalysisCoordinator) Stage(ctx context.Context, who auth.Identity, request RelightAnalysisPlanRequest, prepared PreparedRelightAnalysis) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.DB == nil || c.Reader == nil || c.Bucket == "" || who.UserID != request.Actor || who.SessionID != request.Session {
		return v2input.ErrInvalid
	}
	s := prepared.plan.Snapshot()
	if prepared.params.ID != s.ParamsDocument || prepared.params.SHA256 != s.ParamsSHA {
		return v2input.ErrInvalid
	}
	descriptor, err := prepared.plan.Descriptor(ctx)
	if err != nil {
		return err
	}
	// Compare caller scope and current structural compiler output before any IO.
	structural, err := CompileRelightAnalysisPlan(ctx, request)
	if err != nil {
		return err
	}
	expected, err := structural.Descriptor(ctx)
	if err != nil || !bytes.Equal(descriptor, expected) || !bytes.Equal(s.Config, structural.snapshot.Config) {
		return v2input.ErrInvalid
	}
	rebuilt, err := LoadQualifiedRelightAnalysisPlan(ctx, SQLRelightAnalysisQualificationCatalog{DB: c.DB, Bucket: c.Bucket}, c.Reader, request, c.Expected)
	if err != nil {
		return err
	}
	expected, err = rebuilt.Descriptor(ctx)
	if err != nil || !bytes.Equal(descriptor, expected) {
		return v2input.ErrInvalid
	}
	if err = VerifyAnalysisSourceObjects(ctx, SQLRelightAnalysisArrayCatalog{DB: c.DB, Bucket: c.Bucket}, c.Reader, request); err != nil {
		return err
	}
	repository := relightAnalysisParamsRepository{db: c.DB, bucket: c.Bucket, tenant: request.Tenant, who: who, plan: prepared.plan}
	scope := artifactjson.Scope{Tenant: request.Tenant, Run: s.Run, Owner: s.Task, Purpose: "task_params"}
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
	var staged bool
	if err = tx.QueryRow(ctx, "SELECT photo.stage_v28_relight_analysis_task($1,$2,$3,$4::jsonb)", request.Tenant, s.Run, string(descriptor), proof).Scan(&staged); err != nil {
		return err
	}
	if !staged {
		return v2input.ErrExpired
	}
	return tx.Commit(ctx)
}
