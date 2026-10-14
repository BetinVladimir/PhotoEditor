package interchange

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// Completed producer/object metadata only. Root/report/model qualification is
// an independent semantic policy; physical pixels alone do not establish it.
func (s SQLExportDocumentCatalog) LookupExportSeriesMeasureMask(ctx context.Context, origin ExportSeriesMeasureOrigin, stat v2input.Entry, raw []byte, image ExportClosureNode, maskRef v2input.Ref) (objectstore.Object, error) {
	return lookupExportSeriesMeasureMask(ctx, s, s.DB, s.Tenant, s.Who, s.Request.ProjectID, s.Bucket, origin, stat, raw, image, maskRef)
}
func (s SQLExportResultInputCatalog) LookupExportSeriesMeasureMask(ctx context.Context, origin ExportSeriesMeasureOrigin, stat v2input.Entry, raw []byte, image ExportClosureNode, maskRef v2input.Ref) (objectstore.Object, error) {
	config := s.Export
	return lookupExportSeriesMeasureMask(ctx, s, config.DB, config.Tenant, config.Who, config.Request.ProjectID, config.Bucket, origin, stat, raw, image, maskRef)
}
func lookupExportSeriesMeasureMask(ctx context.Context, authority ExportSeriesMeasureInputAuthority, db *pgxpool.Pool, tenantID uuid.UUID, who auth.Identity, project uuid.UUID, bucketName string, origin ExportSeriesMeasureOrigin, stat v2input.Entry, raw []byte, image ExportClosureNode, maskRef v2input.Ref) (objectstore.Object, error) {
	var zero, out objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if authority == nil || db == nil || tenantID == uuid.Nil || project == uuid.Nil || len(bucketName) < 3 || !validImportRef(maskRef) {
		return zero, v2input.ErrInvalid
	}
	if err := authority.QualifyExportSeriesMeasureInputs(ctx, origin, stat, raw, image); err != nil {
		return zero, err
	}
	var params struct {
		Regions []struct {
			Mask json.RawMessage `json:"mask"`
		} `json:"regions"`
	}
	if json.Unmarshal(raw, &params) != nil {
		return zero, v2input.ErrInvalid
	}
	found := false
	for _, region := range params.Regions {
		mask, err := review.ParseMaskRef(region.Mask)
		if err != nil {
			return zero, err
		}
		if mask.Artifact.ID == maskRef.ArtifactID && mask.Artifact.SHA256 == maskRef.SHA256 {
			found = true
		}
	}
	if !found {
		return zero, v2input.ErrInvalid
	}
	err := tenant.WithTenant(ctx, db, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var bucket string
		err := tx.QueryRow(ctx, `SELECT mask.bucket,mask.object_key,mask.object_version,mask.sha256,mask.bytes,mask.media_type
   FROM photo.artifacts mask
   JOIN photo.tasks producer ON producer.tenant_id=mask.tenant_id AND producer.id=mask.created_by_task_id
   JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
   JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt AND attempt.fencing_token=producer.fencing_token
   JOIN photo.runs run ON run.tenant_id=producer.tenant_id AND run.id=producer.run_id
   LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=run.tenant_id AND workflow.run_id=run.id
   WHERE mask.tenant_id=$1 AND mask.id=$2 AND mask.sha256=$3 AND mask.bucket=$4
   AND mask.media_type IN('application/octet-stream','application/x-npy') AND mask.bytes>0
   AND mask.state='ready' AND mask.verified_at IS NOT NULL AND mask.object_version IS NOT NULL AND mask.object_version NOT IN('','null')
   AND (mask.expires_at IS NULL OR mask.expires_at>clock_timestamp())
   AND producer.operation_version='1' AND producer.state='succeeded' AND attempt.finished_at IS NOT NULL
   AND attempt.backend=producer.execution_profile->>'backend' AND attempt.backend=attempt.profile->>'backend'
   AND ((producer.operation='mask.person' AND mask.output_role='person_mask') OR (producer.operation='mask.face_skin' AND mask.output_role='skin_mask')
    OR (producer.operation='mask.prompt' AND mask.output_role='prompt_mask') OR (producer.operation='mask.transform' AND mask.output_role='transformed_mask'))
   AND mask.produced_attempt=producer.attempt AND mask.produced_fence=producer.fencing_token
   AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',$2::uuid,'sha256',$3::text))
   AND producer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',$5::uuid,'sha256',$6::text))
   AND NOT run.cancel_requested AND (workflow.project_id=$7 OR run.config_snapshot->>'project_id'=$7::text)
   AND photo.v2_editor_authorized($1,$7,$8,$9)`, tenantID, maskRef.ArtifactID, maskRef.SHA256, bucketName, image.Entry.Ref.ArtifactID, image.Entry.Ref.SHA256, project, who.UserID, who.SessionID).Scan(&bucket, &out.Key, &out.VersionID, &out.SHA256, &out.Bytes, &out.ContentType)
		if err != nil {
			return err
		}
		if bucket != bucketName || out.Key == "" || out.SHA256 != maskRef.SHA256 {
			return v2input.ErrInvalid
		}
		return ctx.Err()
	})
	if err != nil {
		return zero, err
	}
	if err := authority.QualifyExportSeriesMeasureInputs(ctx, origin, stat, raw, image); err != nil {
		return zero, err
	}
	return out, ctx.Err()
}

var _ ExportSeriesMeasureMaskSource = SQLExportDocumentCatalog{}
var _ ExportSeriesMeasureMaskSource = SQLExportResultInputCatalog{}
