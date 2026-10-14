package interchange

import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

// Metadata discovery only. Original PersonParams, root/report bytes, installed
// model policy and imported qualification must be verified before this evidence
// can implement ExportStatsPersonSource. Do not treat this locator as admission.
type ExportStatsPersonOrigin struct {
 Root v2input.Ref
 Params v2input.Ref
 Report v2input.Ref
 Mask objectstore.Object
}
func(s SQLExportDocumentCatalog)LookupExportStatsPersonOrigin(ctx context.Context,entry v2input.Entry,paramsRef v2input.Ref,raw []byte,image review.WorkingImage)(ExportStatsPersonOrigin,error){
 var zero,out ExportStatsPersonOrigin
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.DB==nil||len(s.Bucket)<3||len(s.Bucket)>63||entry.Contract!="Stats20"||!validImportRef(entry.Ref)||!validImportRef(paramsRef)||rawSHA(raw)!=paramsRef.SHA256{return zero,v2input.ErrInvalid}
 params,err:=review.ParseStatsParams(raw);if err!=nil{return zero,err}
 imageRaw,err:=canonical.Marshal(image);if err!=nil{return zero,err}
 owned,err:=review.ParseWorkingImage(imageRaw);if err!=nil{return zero,err}
 if rawSHA(imageRaw)!=params.Image.SHA256{return zero,v2input.ErrInvalid}
 if err:=params.ValidateSource(owned,params.Image);err!=nil{return zero,err}
 // The inline StatsParams mask has no root ID. Discover the root from the
 // exact same completed producer as its binary; never choose a supplied root.
 err=tenant.WithTenant(ctx,s.DB,s.Who,s.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var bucket string
  err:=tx.QueryRow(ctx,`SELECT root.id,root.sha256,original.id,original.sha256,report.id,report.sha256,
   mask.bucket,mask.object_key,mask.object_version,mask.sha256,mask.bytes,mask.media_type
   FROM photo.artifacts stats
   JOIN photo.tasks stat_task ON stat_task.tenant_id=stats.tenant_id AND stat_task.id=stats.created_by_task_id
   JOIN photo.artifacts stat_params ON stat_params.tenant_id=stat_task.tenant_id AND stat_params.id=stat_task.params_artifact_id
   JOIN photo.task_results stat_result ON stat_result.tenant_id=stat_task.tenant_id AND stat_result.id=stat_task.canonical_result_id AND stat_result.task_id=stat_task.id AND stat_result.attempt=stat_task.attempt AND stat_result.fencing_token=stat_task.fencing_token
   JOIN photo.artifacts mask ON mask.tenant_id=stats.tenant_id AND mask.id=$6 AND mask.sha256=$7
   JOIN photo.tasks producer ON producer.tenant_id=mask.tenant_id AND producer.id=mask.created_by_task_id
   JOIN photo.artifacts root ON root.tenant_id=producer.tenant_id AND root.created_by_task_id=producer.id AND root.output_role='person_mask_ref'
   JOIN photo.artifacts report ON report.tenant_id=producer.tenant_id AND report.created_by_task_id=producer.id AND report.output_role='person_report'
   JOIN photo.artifacts original ON original.tenant_id=producer.tenant_id AND original.id=producer.params_artifact_id
   JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
   WHERE stats.tenant_id=$1 AND stats.id=$2 AND stats.sha256=$3
   AND stats.output_role='stats' AND stats.produced_attempt=stat_task.attempt AND stats.produced_fence=stat_task.fencing_token
   AND stat_task.operation='color.stats' AND stat_task.operation_version='1' AND stat_task.state='succeeded'
   AND stat_params.id=$4 AND stat_params.sha256=$5
   AND stat_task.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',$6::uuid,'sha256',$7::text),jsonb_build_object('artifact_id',$8::uuid,'sha256',$9::text))
   AND stat_result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',$2::uuid,'sha256',$3::text))
   AND producer.operation='mask.person' AND producer.operation_version='1' AND producer.state='succeeded'
   AND mask.output_role='person_mask' AND mask.media_type IN('application/octet-stream','application/x-npy') AND mask.bytes>0
   AND mask.produced_attempt=producer.attempt AND mask.produced_fence=producer.fencing_token
   AND root.produced_attempt=producer.attempt AND root.produced_fence=producer.fencing_token
   AND report.produced_attempt=producer.attempt AND report.produced_fence=producer.fencing_token
   AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',mask.id,'sha256',mask.sha256),jsonb_build_object('artifact_id',root.id,'sha256',root.sha256),jsonb_build_object('artifact_id',report.id,'sha256',report.sha256))
   AND EXISTS(SELECT 1 FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
    WHERE v.tenant_id=$1 AND a.project_id=$10 AND v.master_artifact_id=$8 AND v.content_hash=$9 AND v.review_state='accepted' AND a.deleted_at IS NULL AND a.upload_state='verified'
    AND photo.semantic_proposal_person_mask_origin($1,$10,$11,$12,v.id,$8,$9,root.id,root.sha256,mask.id,mask.sha256))
   AND NOT EXISTS(SELECT 1 FROM (VALUES(stats.id),(stat_params.id),(mask.id),(root.id),(report.id),(original.id)) required(id)
    JOIN photo.artifacts a ON a.tenant_id=$1 AND a.id=required.id
    WHERE a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null') OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))
   AND root.media_type='application/json' AND root.bytes BETWEEN 2 AND 1048576
   AND original.media_type='application/json' AND original.bytes BETWEEN 2 AND 65536
   AND report.media_type='application/json' AND report.bytes BETWEEN 2 AND 8388608`,
   s.Tenant,entry.Ref.ArtifactID,entry.Ref.SHA256,paramsRef.ArtifactID,paramsRef.SHA256,params.Person.Artifact.ID,params.Person.Artifact.SHA256,params.Image.ID,params.Image.SHA256,s.Request.ProjectID,s.Who.UserID,s.Who.SessionID).Scan(
    &out.Root.ArtifactID,&out.Root.SHA256,&out.Params.ArtifactID,&out.Params.SHA256,&out.Report.ArtifactID,&out.Report.SHA256,
    &bucket,&out.Mask.Key,&out.Mask.VersionID,&out.Mask.SHA256,&out.Mask.Bytes,&out.Mask.ContentType)
  if err!=nil{return err}
  if bucket!=s.Bucket||!validImportRef(out.Root)||!validImportRef(out.Params)||!validImportRef(out.Report)||out.Mask.Key==""||out.Mask.SHA256!=params.Person.Artifact.SHA256{return v2input.ErrInvalid}
  return ctx.Err()
 })
 if err!=nil{return zero,err};return out,ctx.Err()
}
