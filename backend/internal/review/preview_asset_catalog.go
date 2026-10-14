package review

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/objectstore"
)

// Existing media-retry/ingest derivatives can supply the review's before side
// only when their live asset-version master is this exact WorkingImage pin.
func lookupCandidateAssetPreview(ctx context.Context, tx pgx.Tx, tid, project uuid.UUID, source ArtifactRef, variant, profile, bucket string) (CandidatePreviewObjects, error) {
	var out CandidatePreviewObjects
	var manifestBucket, derivativeBucket string
	err := tx.QueryRow(ctx, `SELECT m.id,m.sha256,m.bucket,m.object_key,m.object_version,m.bytes,m.media_type,
 a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.asset_versions v JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
 JOIN photo.derivatives d ON d.tenant_id=v.tenant_id AND d.asset_version_id=v.id
 JOIN photo.tasks t ON t.tenant_id=d.tenant_id AND t.id=d.task_id
 JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id
 JOIN photo.task_results z ON z.tenant_id=t.tenant_id AND z.task_id=t.id AND z.id=t.canonical_result_id
 JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id
 JOIN photo.artifacts m ON m.tenant_id=t.tenant_id AND m.created_by_task_id=t.id AND m.output_role='derivative_manifest'
 JOIN photo.output_uploads mu ON mu.tenant_id=m.tenant_id AND mu.artifact_id=m.id AND mu.task_id=t.id
 JOIN photo.output_uploads iu ON iu.tenant_id=a.tenant_id AND iu.artifact_id=a.id AND iu.task_id=t.id
 WHERE v.tenant_id=$1 AND asset.project_id=$2 AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 AND v.review_state='accepted' AND v.master_artifact_id=$3 AND v.content_hash=$4 AND d.variant=$5 AND d.profile_hash=$6 AND d.status='ready'
 AND t.operation='media.preview' AND t.operation_version='1' AND t.state='succeeded' AND NOT t.cancel_requested
 AND r.status='completed' AND NOT r.cancel_requested AND z.attempt=t.attempt AND z.fencing_token=t.fencing_token
 AND t.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',$3::text,'sha256',$4::text))
 AND jsonb_array_length(z.outputs)=2
 AND z.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',m.id::text,'sha256',m.sha256),jsonb_build_object('artifact_id',a.id::text,'sha256',a.sha256))
 AND m.state='ready' AND a.state='ready' AND m.verified_at IS NOT NULL AND a.verified_at IS NOT NULL
 AND a.created_by_task_id=t.id AND a.output_role='derivative' AND m.produced_attempt=t.attempt AND a.produced_attempt=t.attempt AND m.produced_fence=t.fencing_token AND a.produced_fence=t.fencing_token
 AND mu.state='ready' AND iu.state='ready' AND mu.attempt=t.attempt AND iu.attempt=t.attempt AND mu.fence=t.fencing_token AND iu.fence=t.fencing_token
 AND mu.bucket=m.bucket AND mu.object_key=m.object_key AND mu.object_version=m.object_version AND mu.sha256=m.sha256 AND mu.bytes=m.bytes AND mu.media_type=m.media_type AND mu.role=m.output_role
 AND iu.bucket=a.bucket AND iu.object_key=a.object_key AND iu.object_version=a.object_version AND iu.sha256=a.sha256 AND iu.bytes=a.bytes AND iu.media_type=a.media_type AND iu.role=a.output_role
 AND (m.expires_at IS NULL OR m.expires_at>clock_timestamp()) AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 ORDER BY r.created_at DESC,t.id,m.id LIMIT 1`, tid, project, source.ID, source.SHA256, variant, profile).Scan(&out.Manifest.ID, &out.Manifest.SHA256, &manifestBucket, &out.ManifestObject.Key, &out.ManifestObject.VersionID, &out.ManifestObject.Bytes, &out.ManifestObject.ContentType, &out.Derivative.ID, &out.Derivative.SHA256, &derivativeBucket, &out.DerivativeObject.Key, &out.DerivativeObject.VersionID, &out.DerivativeObject.Bytes, &out.DerivativeObject.ContentType)
	if err != nil {
		return CandidatePreviewObjects{}, err
	}
	out.Source = source
	out.ProfileHash = profile
	out.ManifestObject.SHA256 = out.Manifest.SHA256
	out.DerivativeObject.SHA256 = out.Derivative.SHA256
	if err := validateCandidatePreviewObjects(out, manifestBucket, derivativeBucket, bucket); err != nil {
		return CandidatePreviewObjects{}, err
	}

	return out, nil
}

func validateCandidatePreviewObjects(out CandidatePreviewObjects, manifestBucket, derivativeBucket, bucket string) error {
	if bucket == "" || manifestBucket != bucket || derivativeBucket != bucket || !out.Source.valid() || !out.Manifest.valid() || !out.Derivative.valid() || out.Source.ID == out.Manifest.ID || out.Source.ID == out.Derivative.ID || out.Manifest.ID == out.Derivative.ID || out.ManifestObject.SHA256 != out.Manifest.SHA256 || out.DerivativeObject.SHA256 != out.Derivative.SHA256 || out.ManifestObject.Key == "" || out.DerivativeObject.Key == "" || out.ManifestObject.VersionID == "" || out.ManifestObject.VersionID == "null" || out.DerivativeObject.VersionID == "" || out.DerivativeObject.VersionID == "null" || out.ManifestObject.ContentType != "application/json" || out.ManifestObject.Bytes < 2 || out.ManifestObject.Bytes > 1048576 || out.DerivativeObject.Bytes < 2 || out.DerivativeObject.Bytes > 52*1048576 || (out.DerivativeObject.ContentType != "image/jpeg" && out.DerivativeObject.ContentType != "image/webp") {
		return objectstore.ErrIntegrity
	}
	return nil
}
