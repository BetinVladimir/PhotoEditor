package interchange
import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

type ExportStatsAuthority interface {QualifyExportStats(context.Context,v2input.Entry,[]byte)error}
func(s SQLExportDocumentCatalog)QualifyExportStats(ctx context.Context,entry v2input.Entry,raw []byte)error {
 if err:=ctx.Err();err!=nil{return err}
 if entry.Contract!="Stats20"||!validImportRef(entry.Ref)||rawSHA(raw)!=entry.Ref.SHA256{return v2input.ErrInvalid}
 stats,err:=review.ParseStats20(raw);if err!=nil||stats.SourceImage==nil{return v2input.ErrInvalid}
 object,err:=s.LookupExportDocument(ctx,entry);if err!=nil{return err}
 source:=v2input.Entry{Ref:v2input.Ref{ArtifactID:stats.SourceImage.ID,SHA256:stats.SourceImage.SHA256},Contract:"WorkingImage"}
 if _,err:=s.LookupExportDocument(ctx,source);err!=nil{return err}
 if object.ContentType!="application/json"{return v2input.ErrInvalid}
 return tenant.WithTenant(ctx,s.DB,s.Who,s.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error {
  var allowed bool
  err:=tx.QueryRow(ctx,`SELECT EXISTS(
   SELECT 1 FROM photo.asset_versions v JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
   JOIN photo.artifacts stats ON stats.tenant_id=v.tenant_id AND stats.id=$5 AND stats.sha256=$6
   JOIN photo.tasks producer ON producer.tenant_id=stats.tenant_id AND producer.id=stats.created_by_task_id
   JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id AND params.id=producer.params_artifact_id
   JOIN photo.runs origin ON origin.tenant_id=producer.tenant_id AND origin.id=producer.run_id
   LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=origin.tenant_id AND workflow.run_id=origin.id
   JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt AND attempt.fencing_token=producer.fencing_token
   JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
   WHERE v.tenant_id=$1 AND asset.project_id=$2 AND v.master_artifact_id=$3 AND v.content_hash=$4
   AND v.review_state='accepted' AND asset.deleted_at IS NULL AND asset.upload_state='verified'
   AND photo.v2_editor_authorized($1,$2,$7,$8)
   AND (workflow.project_id=$2 OR origin.config_snapshot->>'project_id'=$2::text)
   AND producer.operation='color.stats' AND producer.operation_version='1' AND producer.state='succeeded' AND producer.resource_class='cpu'
   AND attempt.finished_at IS NOT NULL AND attempt.backend='cpu'
   AND attempt.backend=producer.execution_profile->>'backend' AND attempt.backend=attempt.profile->>'backend'
   AND params.state='ready' AND params.verified_at IS NOT NULL AND params.media_type='application/json' AND params.bytes BETWEEN 2 AND 1048576
   AND params.object_version IS NOT NULL AND params.object_version NOT IN('','null') AND (params.expires_at IS NULL OR params.expires_at>clock_timestamp())
   AND stats.output_role='stats' AND stats.state='ready' AND stats.verified_at IS NOT NULL
   AND stats.produced_attempt=producer.attempt AND stats.produced_fence=producer.fencing_token
   AND stats.object_version IS NOT NULL AND stats.object_version NOT IN('','null') AND (stats.expires_at IS NULL OR stats.expires_at>clock_timestamp())
   AND stats.bucket=$9 AND stats.object_key=$10 AND stats.object_version=$11 AND stats.bytes=$12 AND stats.media_type='application/json'
   AND producer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',$3::uuid,'sha256',$4::text))
   AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',$5::uuid,'sha256',$6::text))
  )`,s.Tenant,s.Request.ProjectID,source.Ref.ArtifactID,source.Ref.SHA256,entry.Ref.ArtifactID,entry.Ref.SHA256,s.Who.UserID,s.Who.SessionID,
   s.Bucket,object.Key,object.VersionID,object.Bytes).Scan(&allowed)
  if err!=nil{return err};if !allowed{return v2input.ErrInvalid};return ctx.Err()
 })
}
var _ ExportStatsAuthority=SQLExportDocumentCatalog{}
