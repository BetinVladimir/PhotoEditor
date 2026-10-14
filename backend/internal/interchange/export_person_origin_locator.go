package interchange

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// LookupPortablePersonOrigin locates current source-side producer metadata.
// It cannot issue a receipt: the caller must separately prove the original
// MaskRef/Params/report bytes, model qualification and recipe occurrence,
// repeat this lookup after object IO, then use a provisioned signing key.
func (s SQLExportDocumentCatalog) LookupPortablePersonOrigin(ctx context.Context,
	source v2input.SessionVersionBinding, root, pixels v2input.Ref,
) (ExportSeriesMaskOrigin, error) {
	var zero, out ExportSeriesMaskOrigin
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Tenant == uuid.Nil || s.Request.ProjectID == uuid.Nil ||
		len(s.Bucket) < 3 || len(s.Bucket) > 63 || source.AssetVersionID == uuid.Nil ||
		!validImportRef(source.Source) || !validImportRef(root) || !validImportRef(pixels) ||
		!portablePersonOriginSHA.MatchString(source.CanvasHash) {
		return zero, v2input.ErrInvalid
	}
	err := tenant.WithTenant(ctx, s.DB, s.Who, s.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var rootBucket, paramsBucket, reportBucket, maskBucket string
		err := tx.QueryRow(ctx, `SELECT producer.id,producer.operation,producer.operation_version,
 producer.attempt,producer.fencing_token,
 root.id,root.sha256,root.bucket,root.object_key,root.object_version,root.bytes,root.media_type,
 params.id,params.sha256,params.bucket,params.object_key,params.object_version,params.bytes,params.media_type,
 report.id,report.sha256,report.bucket,report.object_key,report.object_version,report.bytes,report.media_type,
 mask.bucket,mask.object_key,mask.object_version,mask.bytes,mask.media_type
 FROM photo.artifacts mask
 JOIN photo.tasks producer ON producer.tenant_id=mask.tenant_id AND producer.id=mask.created_by_task_id
 JOIN photo.runs run ON run.tenant_id=producer.tenant_id AND run.id=producer.run_id
 JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id AND attempt.task_id=producer.id
  AND attempt.attempt=producer.attempt AND attempt.fencing_token=producer.fencing_token
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id
  AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 JOIN photo.artifacts root ON root.tenant_id=producer.tenant_id AND root.created_by_task_id=producer.id
  AND root.output_role='person_mask_ref'
 JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id AND params.id=producer.params_artifact_id
 JOIN photo.artifacts report ON report.tenant_id=producer.tenant_id AND report.created_by_task_id=producer.id
  AND report.output_role='person_report'
 JOIN photo.asset_versions version ON version.tenant_id=producer.tenant_id AND version.id=$4
 JOIN photo.assets asset ON asset.tenant_id=version.tenant_id AND asset.id=version.asset_id
 WHERE mask.tenant_id=$1 AND mask.id=$2 AND mask.sha256=$3 AND mask.output_role='person_mask'
 AND root.id=$5 AND root.sha256=$6
 AND version.master_artifact_id=$7 AND version.content_hash=$8 AND asset.project_id=$9
 AND version.review_state='accepted' AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 AND producer.operation='mask.person' AND producer.operation_version='1' AND producer.state='succeeded'
 AND NOT run.cancel_requested AND attempt.finished_at IS NOT NULL
 AND attempt.backend=producer.execution_profile->>'backend' AND attempt.backend=attempt.profile->>'backend'
 AND mask.produced_attempt=producer.attempt AND mask.produced_fence=producer.fencing_token
 AND root.produced_attempt=producer.attempt AND root.produced_fence=producer.fencing_token
 AND report.produced_attempt=producer.attempt AND report.produced_fence=producer.fencing_token
 AND (SELECT count(*) FROM photo.artifacts a WHERE a.tenant_id=$1 AND a.created_by_task_id=producer.id
  AND a.produced_attempt=producer.attempt AND a.produced_fence=producer.fencing_token
  AND a.output_role='person_mask_ref')=1
 AND (SELECT count(*) FROM photo.artifacts a WHERE a.tenant_id=$1 AND a.created_by_task_id=producer.id
  AND a.produced_attempt=producer.attempt AND a.produced_fence=producer.fencing_token
  AND a.output_role='person_report')=1
 AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',mask.id,'sha256',mask.sha256),
  jsonb_build_object('artifact_id',root.id,'sha256',root.sha256),
  jsonb_build_object('artifact_id',report.id,'sha256',report.sha256))
 AND photo.semantic_proposal_person_mask_origin($1,$9,$10,$11,version.id,$7,$8,root.id,root.sha256,mask.id,mask.sha256)
 AND NOT EXISTS(SELECT 1 FROM (VALUES(mask.id),(root.id),(params.id),(report.id)) required(id)
  JOIN photo.artifacts a ON a.tenant_id=$1 AND a.id=required.id
  WHERE a.state IS DISTINCT FROM 'ready' OR a.verified_at IS NULL OR a.object_version IS NULL
  OR a.object_version IN('','null') OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))
 AND root.media_type='application/json' AND root.bytes BETWEEN 2 AND 1048576
 AND params.media_type='application/json' AND params.bytes BETWEEN 2 AND 65536
 AND report.media_type='application/json' AND report.bytes BETWEEN 2 AND 8388608
 AND mask.media_type IN('application/octet-stream','application/x-npy') AND mask.bytes>0`,
			s.Tenant, pixels.ArtifactID, pixels.SHA256, source.AssetVersionID,
			root.ArtifactID, root.SHA256, source.Source.ArtifactID, source.Source.SHA256,
			s.Request.ProjectID, s.Who.UserID, s.Who.SessionID).Scan(
			&out.Task, &out.Operation, &out.Version, &out.Attempt, &out.Fence,
			&out.Root.Ref.ArtifactID, &out.Root.Ref.SHA256, &rootBucket,
			&out.Root.Object.Key, &out.Root.Object.VersionID, &out.Root.Object.Bytes, &out.Root.Object.ContentType,
			&out.Params.Ref.ArtifactID, &out.Params.Ref.SHA256, &paramsBucket,
			&out.Params.Object.Key, &out.Params.Object.VersionID, &out.Params.Object.Bytes, &out.Params.Object.ContentType,
			&out.Report.Ref.ArtifactID, &out.Report.Ref.SHA256, &reportBucket,
			&out.Report.Object.Key, &out.Report.Object.VersionID, &out.Report.Object.Bytes, &out.Report.Object.ContentType,
			&maskBucket, &out.Mask.Key, &out.Mask.VersionID, &out.Mask.Bytes, &out.Mask.ContentType)
		if err != nil {
			return err
		}
		out.Root.Object.SHA256 = out.Root.Ref.SHA256
		out.Params.Object.SHA256 = out.Params.Ref.SHA256
		out.Report.Object.SHA256 = out.Report.Ref.SHA256
		out.Mask.SHA256 = pixels.SHA256
		if rootBucket != s.Bucket || paramsBucket != s.Bucket || reportBucket != s.Bucket || maskBucket != s.Bucket ||
			out.Task == uuid.Nil || out.Attempt < 1 || out.Fence < 1 || out.Root.Ref != root ||
			!validImportRef(out.Params.Ref) || !validImportRef(out.Report.Ref) ||
			out.Root.Object.Key == "" || out.Params.Object.Key == "" || out.Report.Object.Key == "" || out.Mask.Key == "" ||
			out.Root.Object.VersionID == "" || out.Params.Object.VersionID == "" ||
			out.Report.Object.VersionID == "" || out.Mask.VersionID == "" ||
			out.Root.Object.VersionID == "null" || out.Params.Object.VersionID == "null" ||
			out.Report.Object.VersionID == "null" || out.Mask.VersionID == "null" {
			return v2input.ErrInvalid
		}
		return ctx.Err()
	})
	if err != nil {
		return zero, err
	}
	return out, ctx.Err()
}
