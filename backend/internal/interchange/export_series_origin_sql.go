package interchange

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

func (s SQLExportDocumentCatalog) LookupExportSeriesOrigin(ctx context.Context, entry v2input.Entry, raw []byte) (ExportSeriesOriginEvidence, error) {
	return lookupExportSeriesOrigin(ctx, s, s.DB, s.Tenant, s.Who, s.Request.ProjectID, s.Bucket, entry, raw)
}
func (s SQLExportResultInputCatalog) LookupExportSeriesOrigin(ctx context.Context, entry v2input.Entry, raw []byte) (ExportSeriesOriginEvidence, error) {
	config := s.Export
	return lookupExportSeriesOrigin(ctx, s, config.DB, config.Tenant, config.Who, config.Request.ProjectID, config.Bucket, entry, raw)
}

type exportSeriesOriginCatalog interface {
	ExportSeriesAuthority
	ExportDocumentCatalog
}

func lookupExportSeriesOrigin(ctx context.Context, catalog exportSeriesOriginCatalog, db *pgxpool.Pool, tenantID uuid.UUID, who auth.Identity, project uuid.UUID, bucketName string, entry v2input.Entry, raw []byte) (ExportSeriesOriginEvidence, error) {
	var zero, out ExportSeriesOriginEvidence
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if catalog == nil || db == nil || tenantID == uuid.Nil || project == uuid.Nil || len(bucketName) < 3 {
		return zero, v2input.ErrInvalid
	}
	if err := catalog.QualifyExportSeries(ctx, entry, raw); err != nil {
		return zero, err
	}
	object, err := catalog.LookupExportDocument(ctx, entry)
	if err != nil {
		return zero, err
	}
	err = tenant.WithTenant(ctx, db, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var bucket string
		err := tx.QueryRow(ctx, `SELECT producer.id,producer.attempt,producer.fencing_token,params.id,params.sha256,params.bucket,params.object_key,params.object_version,params.sha256,params.bytes,params.media_type FROM photo.artifacts series
   JOIN photo.tasks producer ON producer.tenant_id=series.tenant_id AND producer.id=series.created_by_task_id
   JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id AND params.id=producer.params_artifact_id
   JOIN photo.runs origin ON origin.tenant_id=producer.tenant_id AND origin.id=producer.run_id
   LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=origin.tenant_id AND workflow.run_id=origin.id
   JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
   JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt AND attempt.fencing_token=producer.fencing_token
   WHERE series.tenant_id=$1 AND series.id=$2 AND series.sha256=$3
   AND producer.operation='series.balance' AND producer.operation_version='1' AND producer.state='succeeded' AND producer.resource_class='cpu'
   AND attempt.finished_at IS NOT NULL AND attempt.backend='cpu' AND attempt.profile->>'backend'='cpu' AND producer.execution_profile->>'backend'='cpu'
   AND NOT origin.cancel_requested AND (workflow.project_id=$4 OR origin.config_snapshot->>'project_id'=$4::text)
   AND photo.v2_editor_authorized($1,$4,$5,$6)
   AND series.output_role='series_corrections' AND series.produced_attempt=producer.attempt AND series.produced_fence=producer.fencing_token
   AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',$2::uuid,'sha256',$3::text))
   AND series.bucket=$7 AND series.object_key=$8 AND series.object_version=$9 AND series.bytes=$10 AND series.media_type='application/json'
   AND params.media_type='application/json' AND params.bytes BETWEEN 2 AND 1048576
   AND NOT EXISTS(SELECT 1 FROM (VALUES(series.id),(params.id)) required(id) JOIN photo.artifacts a ON a.tenant_id=$1 AND a.id=required.id
    WHERE a.state IS DISTINCT FROM 'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null') OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))`, tenantID, entry.Ref.ArtifactID, entry.Ref.SHA256, project, who.UserID, who.SessionID, bucketName, object.Key, object.VersionID, object.Bytes).Scan(&out.Task, &out.Attempt, &out.Fence, &out.Params.ArtifactID, &out.Params.SHA256, &bucket, &out.Object.Key, &out.Object.VersionID, &out.Object.SHA256, &out.Object.Bytes, &out.Object.ContentType)
		if err != nil {
			return err
		}
		if bucket != bucketName || !validImportRef(out.Params) || out.Object.SHA256 != out.Params.SHA256 || out.Object.Key == "" {
			return v2input.ErrInvalid
		}
		return ctx.Err()
	})
	if err != nil {
		return zero, err
	}
	if err := catalog.QualifyExportSeries(ctx, entry, raw); err != nil {
		return zero, err
	}
	return out, ctx.Err()
}

// Authorizes only an exact original balance input backed by its own completed
// series.measure producer. Document content and accepted image/mask semantics
// are independent gates, not inferred from metadata here.
func (s SQLExportDocumentCatalog) LookupExportSeriesOriginDocument(ctx context.Context, origin ExportSeriesOriginEvidence, entry v2input.Entry) (objectstore.Object, error) {
	return lookupExportSeriesOriginDocument(ctx, s.DB, s.Tenant, s.Who, s.Request.ProjectID, s.Bucket, origin, entry)
}
func (s SQLExportResultInputCatalog) LookupExportSeriesOriginDocument(ctx context.Context, origin ExportSeriesOriginEvidence, entry v2input.Entry) (objectstore.Object, error) {
	config := s.Export
	var zero objectstore.Object
	if !validImportRef(s.Root) {
		return zero, v2input.ErrInvalid
	}
	root := v2input.Entry{Ref: s.Root, Contract: "InterchangeResult"}
	before, err := config.LookupExportOutput(ctx, root)
	if err != nil {
		return zero, err
	}
	object, err := lookupExportSeriesOriginDocument(ctx, config.DB, config.Tenant, config.Who, config.Request.ProjectID, config.Bucket, origin, entry)
	if err != nil {
		return zero, err
	}
	after, err := config.LookupExportOutput(ctx, root)
	if err != nil {
		return zero, err
	}
	if after != before {
		return zero, v2input.ErrInvalid
	}
	return object, ctx.Err()
}
func lookupExportSeriesOriginDocument(ctx context.Context, db *pgxpool.Pool, tenantID uuid.UUID, who auth.Identity, project uuid.UUID, bucketName string, origin ExportSeriesOriginEvidence, entry v2input.Entry) (objectstore.Object, error) {
	var zero, out objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if db == nil || tenantID == uuid.Nil || project == uuid.Nil || len(bucketName) < 3 || entry.Contract != "SeriesStats" || !validImportRef(entry.Ref) || !validImportRef(origin.Params) || origin.Attempt < 1 || origin.Fence < 1 {
		return zero, v2input.ErrInvalid
	}
	err := tenant.WithTenant(ctx, db, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var bucket string
		err := tx.QueryRow(ctx, `SELECT stats.bucket,stats.object_key,stats.object_version,stats.sha256,stats.bytes,stats.media_type
   FROM photo.tasks balance
   JOIN photo.artifacts params ON params.tenant_id=balance.tenant_id AND params.id=balance.params_artifact_id
   JOIN photo.runs balance_run ON balance_run.tenant_id=balance.tenant_id AND balance_run.id=balance.run_id
   LEFT JOIN photo.v2_workflows balance_workflow ON balance_workflow.tenant_id=balance_run.tenant_id AND balance_workflow.run_id=balance_run.id
   JOIN photo.task_results balance_result ON balance_result.tenant_id=balance.tenant_id AND balance_result.id=balance.canonical_result_id AND balance_result.task_id=balance.id AND balance_result.attempt=balance.attempt AND balance_result.fencing_token=balance.fencing_token
   JOIN photo.artifacts stats ON stats.tenant_id=balance.tenant_id AND stats.id=$5 AND stats.sha256=$6
   JOIN photo.tasks measure ON measure.tenant_id=stats.tenant_id AND measure.id=stats.created_by_task_id
   JOIN photo.runs measure_run ON measure_run.tenant_id=measure.tenant_id AND measure_run.id=measure.run_id
   LEFT JOIN photo.v2_workflows measure_workflow ON measure_workflow.tenant_id=measure_run.tenant_id AND measure_workflow.run_id=measure_run.id
   JOIN photo.task_results result ON result.tenant_id=measure.tenant_id AND result.id=measure.canonical_result_id AND result.task_id=measure.id AND result.attempt=measure.attempt AND result.fencing_token=measure.fencing_token
   JOIN photo.task_attempts attempt ON attempt.tenant_id=measure.tenant_id AND attempt.task_id=measure.id AND attempt.attempt=measure.attempt AND attempt.fencing_token=measure.fencing_token
   WHERE balance.tenant_id=$1 AND balance.id=$2 AND balance.attempt=$3 AND balance.fencing_token=$4
   AND balance.operation='series.balance' AND balance.operation_version='1' AND balance.state='succeeded'
   AND balance.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',$5::uuid,'sha256',$6::text))
   AND params.id=$7 AND params.sha256=$8 AND params.bucket=$9 AND params.object_key=$10 AND params.object_version=$11 AND params.bytes=$12 AND params.media_type='application/json'
   AND stats.bucket=$9 AND stats.bytes BETWEEN 2 AND 1048576 AND stats.media_type='application/json'
   AND measure.operation='series.measure' AND measure.operation_version='1' AND measure.state='succeeded' AND measure.resource_class='cpu'
   AND attempt.finished_at IS NOT NULL AND attempt.backend='cpu' AND attempt.profile->>'backend'='cpu' AND measure.execution_profile->>'backend'='cpu'
   AND stats.output_role='series_stats' AND stats.produced_attempt=measure.attempt AND stats.produced_fence=measure.fencing_token
   AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',$5::uuid,'sha256',$6::text))
   AND NOT balance_run.cancel_requested AND NOT measure_run.cancel_requested
   AND (balance_workflow.project_id=$13 OR balance_run.config_snapshot->>'project_id'=$13::text)
   AND (measure_workflow.project_id=$13 OR measure_run.config_snapshot->>'project_id'=$13::text)
   AND photo.v2_editor_authorized($1,$13,$14,$15)
   AND NOT EXISTS(SELECT 1 FROM (VALUES(params.id),(stats.id)) required(id) JOIN photo.artifacts a ON a.tenant_id=$1 AND a.id=required.id
    WHERE a.state IS DISTINCT FROM 'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null') OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))`, tenantID, origin.Task, origin.Attempt, origin.Fence, entry.Ref.ArtifactID, entry.Ref.SHA256, origin.Params.ArtifactID, origin.Params.SHA256, bucketName, origin.Object.Key, origin.Object.VersionID, origin.Object.Bytes, project, who.UserID, who.SessionID).Scan(&bucket, &out.Key, &out.VersionID, &out.SHA256, &out.Bytes, &out.ContentType)
		if err != nil {
			return err
		}
		if bucket != bucketName || out.Key == "" || out.SHA256 != entry.Ref.SHA256 {
			return v2input.ErrInvalid
		}
		return ctx.Err()
	})
	if err != nil {
		return zero, err
	}
	return out, ctx.Err()
}

var _ ExportSeriesOriginSource = SQLExportDocumentCatalog{}
var _ ExportSeriesOriginSource = SQLExportResultInputCatalog{}
