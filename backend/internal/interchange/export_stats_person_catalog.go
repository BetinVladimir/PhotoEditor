package interchange

import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

func(s SQLExportDocumentCatalog)LookupExportStatsPersonDocument(ctx context.Context,origin ExportStatsPersonOrigin,entry v2input.Entry)(objectstore.Object,error){
 var zero,out objectstore.Object
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.DB==nil||len(s.Bucket)<3||len(s.Bucket)>63||!validImportRef(origin.Root)||!validImportRef(origin.Params)||!validImportRef(origin.Report)||!validImportRef(entry.Ref){return zero,v2input.ErrInvalid}
 var expected v2input.Ref
 var maximum int64
 switch entry.Contract{case "MaskRef":expected=origin.Root;maximum=1<<20;case "PersonParams":expected=origin.Params;maximum=65536;case "PersonReport":expected=origin.Report;maximum=1<<20;default:return zero,v2input.ErrInvalid}
 if entry.Ref!=expected||origin.Root.ArtifactID==origin.Params.ArtifactID||origin.Root.ArtifactID==origin.Report.ArtifactID||origin.Params.ArtifactID==origin.Report.ArtifactID||origin.Mask.Key==""||origin.Mask.VersionID==""||origin.Mask.VersionID=="null"||origin.Mask.Bytes<1||!exportArrayMediaType(origin.Mask.ContentType){return zero,v2input.ErrInvalid}
 intent,err:=FreezeCreateIntent(s.Tenant,s.Who,s.Request,s.Key);if err!=nil{return zero,err}
 if _,_,err:=DecodeFrozenExportDocuments(ctx,intent,s.Frozen.Body());err!=nil{return zero,err}
 err=tenant.WithTenant(ctx,s.DB,s.Who,s.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var bucket string
  err:=tx.QueryRow(ctx,`SELECT selected.bucket,selected.object_key,selected.object_version,selected.sha256,selected.bytes,selected.media_type
   FROM photo.artifacts root
   JOIN photo.tasks producer ON producer.tenant_id=root.tenant_id AND producer.id=root.created_by_task_id
   JOIN photo.runs run_row ON run_row.tenant_id=producer.tenant_id AND run_row.id=producer.run_id
   LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=run_row.tenant_id AND workflow.run_id=run_row.id
   JOIN photo.artifacts original ON original.tenant_id=producer.tenant_id AND original.id=producer.params_artifact_id
   JOIN photo.artifacts report ON report.tenant_id=producer.tenant_id AND report.created_by_task_id=producer.id AND report.id=$6 AND report.sha256=$7
   JOIN photo.artifacts mask ON mask.tenant_id=producer.tenant_id AND mask.created_by_task_id=producer.id AND mask.sha256=$8
   JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
   JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt AND attempt.fencing_token=producer.fencing_token
   JOIN photo.artifacts selected ON selected.tenant_id=root.tenant_id AND selected.id=$9 AND selected.sha256=$10
   WHERE root.tenant_id=$1 AND root.id=$2 AND root.sha256=$3 AND original.id=$4 AND original.sha256=$5
   AND selected.id IN(root.id,original.id,report.id) AND selected.media_type='application/json' AND selected.bytes BETWEEN 2 AND $11
   AND producer.operation='mask.person' AND producer.operation_version='1' AND producer.state='succeeded' AND NOT run_row.cancel_requested
   AND attempt.finished_at IS NOT NULL AND attempt.backend=producer.execution_profile->>'backend' AND attempt.backend=attempt.profile->>'backend'
   AND root.output_role='person_mask_ref' AND report.output_role='person_report' AND mask.output_role='person_mask'
   AND root.produced_attempt=producer.attempt AND root.produced_fence=producer.fencing_token
   AND report.produced_attempt=producer.attempt AND report.produced_fence=producer.fencing_token
   AND mask.produced_attempt=producer.attempt AND mask.produced_fence=producer.fencing_token
   AND mask.bucket=$12 AND mask.object_key=$13 AND mask.object_version=$14 AND mask.bytes=$15 AND mask.media_type=$16
   AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',root.id,'sha256',root.sha256),jsonb_build_object('artifact_id',report.id,'sha256',report.sha256),jsonb_build_object('artifact_id',mask.id,'sha256',mask.sha256))
   AND (workflow.project_id=$17 OR run_row.config_snapshot->>'project_id'=$17::text)
   AND photo.v2_editor_authorized($1,$17,$18,$19)
   AND NOT EXISTS(SELECT 1 FROM (VALUES(root.id),(original.id),(report.id),(mask.id)) required(id)
    JOIN photo.artifacts a ON a.tenant_id=$1 AND a.id=required.id
    WHERE a.state<>'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null') OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))`,
   s.Tenant,origin.Root.ArtifactID,origin.Root.SHA256,origin.Params.ArtifactID,origin.Params.SHA256,origin.Report.ArtifactID,origin.Report.SHA256,origin.Mask.SHA256,
   entry.Ref.ArtifactID,entry.Ref.SHA256,maximum,s.Bucket,origin.Mask.Key,origin.Mask.VersionID,origin.Mask.Bytes,origin.Mask.ContentType,s.Request.ProjectID,s.Who.UserID,s.Who.SessionID).Scan(
   &bucket,&out.Key,&out.VersionID,&out.SHA256,&out.Bytes,&out.ContentType)
  if err!=nil{return err};if bucket!=s.Bucket||out.Key==""||out.SHA256!=entry.Ref.SHA256{return v2input.ErrInvalid};return ctx.Err()
 })
 if err!=nil{return zero,err};return out,ctx.Err()
}
var _ ExportStatsPersonDocumentCatalog=SQLExportDocumentCatalog{}
