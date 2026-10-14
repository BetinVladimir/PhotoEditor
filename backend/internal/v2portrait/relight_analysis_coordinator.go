package v2portrait

import (
	"context"

	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// RelightAnalysisCoordinator is a deployment-installed producer preparation
// boundary. Expected must be independently provisioned; it is not request data.
// Preparing Params does not stage, seal or admit the producer task.
type RelightAnalysisCoordinator struct {
	DB       DonorConfirmationTransactions
	Reader   v2input.SessionAnalysisObjectReader
	Writer   artifactjson.Objects
	Bucket   string
	Expected v2qualification.Expected
}

type PreparedRelightAnalysis struct {
	plan   RelightAnalysisPlan
	params artifactjson.Ref
}

func (p PreparedRelightAnalysis) Plan() RelightAnalysisPlan {
	return RelightAnalysisPlan{snapshot: p.plan.Snapshot()}
}
func (p PreparedRelightAnalysis) Params() artifactjson.Ref { return p.params }

// PrepareParams verifies original qualification bytes before creating a draft
// or reserving storage, then publishes immutable Params through the dedicated
// stored-plan authority. Retrying an identical request retains all identities.
func (c RelightAnalysisCoordinator) PrepareParams(ctx context.Context, who auth.Identity, request RelightAnalysisPlanRequest) (PreparedRelightAnalysis, error) {
	var zero PreparedRelightAnalysis
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if c.DB == nil || c.Reader == nil || c.Writer == nil || c.Bucket == "" || len(c.Bucket) > 255 || who.UserID != request.Actor || who.SessionID != request.Session {
		return zero, v2input.ErrInvalid
	}
	// Pure structural compilation rejects malformed source/configuration before
	// opening a catalog transaction. It does not substitute for report verification.
	if _, err := CompileRelightAnalysisPlan(ctx, request); err != nil {
		return zero, err
	}
	expected := c.Expected
	runtime := request.Runtime
	if expected.Operation != request.Operation || expected.Version != "1" || expected.Backend != runtime.Backend || expected.ModelLockSHA != runtime.ModelLockSHA || expected.RuntimeLockSHA != runtime.RuntimeLockSHA || expected.DeviceProfileSHA != runtime.DeviceProfileSHA || !expected.LicenseCleared {
		return zero, v2qualification.ErrUnqualified
	}
	catalog := SQLRelightAnalysisQualificationCatalog{DB: c.DB, Bucket: c.Bucket}
	plan, err := LoadQualifiedRelightAnalysisPlan(ctx, catalog, c.Reader, request, expected)
	if err != nil {
		return zero, err
	}
	if err = VerifyAnalysisSourceObjects(ctx, SQLRelightAnalysisArrayCatalog{DB: c.DB, Bucket: c.Bucket}, c.Reader, request); err != nil {
		return zero, err
	}
	// Descriptor validation precedes any draft write as well.
	if _, err = plan.DraftTask(ctx); err != nil {
		return zero, err
	}
	params, err := PublishRelightAnalysisParams(ctx, c.DB, c.Writer, c.Bucket, who, request.Tenant, plan)
	if err != nil {
		return zero, err
	}
	snapshot := plan.Snapshot()
	if params.ID != snapshot.ParamsDocument || params.SHA256 != snapshot.ParamsSHA {
		return zero, v2input.ErrInvalid
	}
	return PreparedRelightAnalysis{plan: plan, params: params}, nil
}
