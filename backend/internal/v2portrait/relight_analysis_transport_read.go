package v2portrait

import (
	"bytes"
	"context"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"time"
)

// ReadPublishedTransport restores committed documents without publication,
// quota repair, input registration or activation. Original report verification
// happens outside the transaction; SQL repeats current stored-plan authority.
func (c RelightAnalysisCoordinator) ReadPublishedTransport(ctx context.Context, who auth.Identity, request RelightAnalysisPlanRequest, prepared PreparedRelightAnalysis) (v2input.GraphTask, error) {
	var zero v2input.GraphTask
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if c.DB == nil || c.Reader == nil || c.Bucket == "" || who.UserID != request.Actor || who.SessionID != request.Session {
		return zero, v2input.ErrInvalid
	}
	s := prepared.plan.Snapshot()
	if prepared.params.ID != s.ParamsDocument || prepared.params.SHA256 != s.ParamsSHA {
		return zero, v2input.ErrInvalid
	}
	descriptor, err := prepared.plan.Descriptor(ctx)
	if err != nil {
		return zero, err
	}
	// Compare caller scope and current structural compiler output before any IO.
	structural, err := CompileRelightAnalysisPlan(ctx, request)
	if err != nil {
		return zero, err
	}
	expected, err := structural.Descriptor(ctx)
	if err != nil || !bytes.Equal(descriptor, expected) || !bytes.Equal(s.Config, structural.snapshot.Config) {
		return zero, v2input.ErrInvalid
	}
	rebuilt, err := LoadQualifiedRelightAnalysisPlan(ctx, SQLRelightAnalysisQualificationCatalog{DB: c.DB, Bucket: c.Bucket}, c.Reader, request, c.Expected)
	if err != nil {
		return zero, err
	}
	expected, err = rebuilt.Descriptor(ctx)
	if err != nil || !bytes.Equal(descriptor, expected) {
		return zero, v2input.ErrInvalid
	}
	if err = VerifyAnalysisSourceObjects(ctx, SQLRelightAnalysisArrayCatalog{DB: c.DB, Bucket: c.Bucket}, c.Reader, request); err != nil {
		return zero, err
	}
	repository := relightAnalysisParamsRepository{db: c.DB, bucket: c.Bucket, tenant: request.Tenant, who: who, plan: prepared.plan}
	scope := artifactjson.Scope{Tenant: request.Tenant, Run: s.Run, Owner: s.Task, Purpose: "task_params"}
	proof, err := repository.proof(scope, s.ParamsSHA, int64(len(s.Params)))
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
	if err = tx.QueryRow(ctx, "SELECT photo.read_v28_relight_analysis_published_transport($1,$2,$3,$4,$5::jsonb)", request.Tenant, s.Run, s.Task, string(descriptor), proof).Scan(&raw); err != nil {
		return zero, err
	}
	graph, err := prepared.plan.RestorePublishedTransport(ctx, raw)
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return graph, nil
}
