package interchange
import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

// Metadata only. Original mask documents/current producer policy and native
// physical content remain independent mandatory gates.
func(s SQLExportDocumentCatalog)LookupExportAlignmentBackgroundOrigin(ctx context.Context,origin ExportBodyDependencyOrigin,parent v2input.Entry,raw []byte,fit ExportClosureNode,alignment v2input.Entry,params []byte,image ExportClosureNode)(ExportSeriesMaskOrigin,error){
 var zero,out ExportSeriesMaskOrigin
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.DB==nil{return zero,v2input.ErrInvalid}
 projected,err:=exportAlignmentBackgroundProjection(origin,parent,raw,fit,alignment,params,image);if err!=nil{return zero,err}
 ref:=v2input.Ref{ArtifactID:projected.Artifact.ID,SHA256:projected.Artifact.SHA256}
 mask,err:=s.lookupExportAlignmentBackgroundObject(ctx,origin,parent,raw,fit,alignment,params,image);if err!=nil{return zero,err}
 out.Mask=mask
 err=tenant.WithTenant(ctx,s.DB,s.Who,s.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var rootBucket,paramsBucket,reportBucket string
  err:=tx.QueryRow(ctx,`SELECT producer.id,producer.operation,producer.operation_version,producer.attempt,producer.fencing_token,
   root.id,root.sha256,root.bucket,root.object_key,root.object_version,root.sha256,root.bytes,root.media_type,
   params.id,params.sha256,params.bucket,params.object_key,params.object_version,params.sha256,params.bytes,params.media_type,
   report.id,report.sha256,report.bucket,report.object_key,report.object_version,report.sha256,report.bytes,report.media_type
   FROM photo.artifacts mask
   JOIN photo.tasks producer ON producer.tenant_id=mask.tenant_id AND producer.id=mask.created_by_task_id
   JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id AND params.id=producer.params_artifact_id
   JOIN photo.artifacts root ON root.tenant_id=producer.tenant_id AND root.created_by_task_id=producer.id
    AND root.output_role=CASE producer.operation WHEN 'mask.person' THEN 'person_mask_ref' WHEN 'mask.prompt' THEN 'prompt_mask_ref' END
   JOIN photo.artifacts report ON report.tenant_id=producer.tenant_id AND report.created_by_task_id=producer.id
    AND report.output_role=CASE producer.operation WHEN 'mask.person' THEN 'person_report' WHEN 'mask.prompt' THEN 'prompt_mask_report' END
   JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
   JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt AND attempt.fencing_token=producer.fencing_token
   JOIN photo.runs run ON run.tenant_id=producer.tenant_id AND run.id=producer.run_id
   LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=run.tenant_id AND workflow.run_id=run.id
   WHERE mask.tenant_id=$1 AND mask.id=$2 AND mask.sha256=$3 AND mask.bucket=$4 AND mask.object_key=$5 AND mask.object_version=$6 AND mask.bytes=$7
   AND producer.operation IN('mask.person','mask.prompt') AND producer.operation_version='1' AND producer.state='succeeded'
   AND mask.output_role=CASE producer.operation WHEN 'mask.person' THEN 'person_mask' WHEN 'mask.prompt' THEN 'prompt_mask' END
   AND mask.produced_attempt=producer.attempt AND mask.produced_fence=producer.fencing_token
   AND root.produced_attempt=producer.attempt AND root.produced_fence=producer.fencing_token
   AND report.produced_attempt=producer.attempt AND report.produced_fence=producer.fencing_token
   AND 1=(SELECT count(*) FROM photo.artifacts a WHERE a.tenant_id=producer.tenant_id AND a.created_by_task_id=producer.id AND a.output_role=root.output_role AND a.produced_attempt=producer.attempt AND a.produced_fence=producer.fencing_token)
   AND 1=(SELECT count(*) FROM photo.artifacts a WHERE a.tenant_id=producer.tenant_id AND a.created_by_task_id=producer.id AND a.output_role=report.output_role AND a.produced_attempt=producer.attempt AND a.produced_fence=producer.fencing_token)
   AND attempt.finished_at IS NOT NULL AND attempt.backend=producer.execution_profile->>'backend' AND attempt.backend=attempt.profile->>'backend'
   AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',mask.id,'sha256',mask.sha256),jsonb_build_object('artifact_id',root.id,'sha256',root.sha256),jsonb_build_object('artifact_id',report.id,'sha256',report.sha256))
   AND NOT run.cancel_requested AND (workflow.project_id=$8 OR run.config_snapshot->>'project_id'=$8::text)
   AND photo.v2_editor_authorized($1,$8,$9,$10)
   AND `+exportPromptWorkflowConsentSQL+`
   AND NOT EXISTS(SELECT 1 FROM (VALUES(root.id),(params.id),(report.id)) required(id) JOIN photo.artifacts a ON a.tenant_id=$1 AND a.id=required.id
    WHERE a.state IS DISTINCT FROM 'ready' OR a.verified_at IS NULL OR a.media_type IS DISTINCT FROM 'application/json' OR a.bytes NOT BETWEEN 2 AND 1048576
    OR a.bucket IS DISTINCT FROM $4 OR a.object_version IS NULL OR a.object_version IN('','null') OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp()))`,s.Tenant,ref.ArtifactID,ref.SHA256,s.Bucket,mask.Key,mask.VersionID,mask.Bytes,s.Request.ProjectID,s.Who.UserID,s.Who.SessionID).Scan(
   &out.Task,&out.Operation,&out.Version,&out.Attempt,&out.Fence,
   &out.Root.Ref.ArtifactID,&out.Root.Ref.SHA256,&rootBucket,&out.Root.Object.Key,&out.Root.Object.VersionID,&out.Root.Object.SHA256,&out.Root.Object.Bytes,&out.Root.Object.ContentType,
   &out.Params.Ref.ArtifactID,&out.Params.Ref.SHA256,&paramsBucket,&out.Params.Object.Key,&out.Params.Object.VersionID,&out.Params.Object.SHA256,&out.Params.Object.Bytes,&out.Params.Object.ContentType,
   &out.Report.Ref.ArtifactID,&out.Report.Ref.SHA256,&reportBucket,&out.Report.Object.Key,&out.Report.Object.VersionID,&out.Report.Object.SHA256,&out.Report.Object.Bytes,&out.Report.Object.ContentType)
  if err!=nil{return err}
  if rootBucket!=s.Bucket||paramsBucket!=s.Bucket||reportBucket!=s.Bucket{return v2input.ErrInvalid}
  seen:=map[uuid.UUID]bool{ref.ArtifactID:true}
  for _,document:=range []ExportSeriesMaskDocument{out.Root,out.Params,out.Report}{if !validImportRef(document.Ref)||seen[document.Ref.ArtifactID]||document.Object.Key==""||document.Object.SHA256!=document.Ref.SHA256{return v2input.ErrInvalid};seen[document.Ref.ArtifactID]=true}
  return ctx.Err()
 })
 if err!=nil{return zero,err}
 current,err:=s.lookupExportAlignmentBackgroundObject(ctx,origin,parent,raw,fit,alignment,params,image);if err!=nil{return zero,err};if current!=mask{return zero,v2input.ErrInvalid};return out,ctx.Err()
}
func(s SQLExportDocumentCatalog)lookupExportAlignmentBackgroundObject(ctx context.Context,origin ExportBodyDependencyOrigin,parent v2input.Entry,raw []byte,fit ExportClosureNode,alignment v2input.Entry,params []byte,image ExportClosureNode)(objectstore.Object,error){
 var zero,out objectstore.Object
 if err:=ctx.Err();err!=nil{return zero,err};if s.DB==nil{return zero,v2input.ErrInvalid}
 projected,err:=exportAlignmentBackgroundProjection(origin,parent,raw,fit,alignment,params,image);if err!=nil{return zero,err}
 current,err:=s.LookupExportBodyFitAlignmentOrigin(ctx,parent,raw,fit,alignment);if err!=nil{return zero,err};if current!=origin{return zero,v2input.ErrInvalid}
 err=tenant.WithTenant(ctx,s.DB,s.Who,s.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var bucket string
  err:=tx.QueryRow(ctx,`SELECT a.bucket,a.object_key,a.object_version,a.sha256,a.bytes,a.media_type
   FROM photo.artifacts a JOIN photo.tasks consumer ON consumer.tenant_id=a.tenant_id AND consumer.id=$4
   WHERE a.tenant_id=$1 AND a.id=$2 AND a.sha256=$3 AND a.state='ready' AND a.verified_at IS NOT NULL
   AND a.media_type IN('application/octet-stream','application/x-npy') AND a.bytes BETWEEN 1 AND 800065536
   AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null') AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
   AND consumer.operation='pair.register' AND consumer.operation_version='1' AND consumer.state='succeeded'
   AND consumer.attempt=$5 AND consumer.fencing_token=$6 AND consumer.params_artifact_id=$7
   AND consumer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',$2::uuid,'sha256',$3::text))
   AND photo.v2_editor_authorized($1,$8,$9,$10)`,s.Tenant,projected.Artifact.ID,projected.Artifact.SHA256,origin.Producer.Task,origin.Producer.Attempt,origin.Producer.Fence,origin.Params.ArtifactID,s.Request.ProjectID,s.Who.UserID,s.Who.SessionID).Scan(&bucket,&out.Key,&out.VersionID,&out.SHA256,&out.Bytes,&out.ContentType)
  if err!=nil{return err};if bucket!=s.Bucket||out.Key==""{return v2input.ErrInvalid};return ctx.Err()
 });if err!=nil{return zero,err}
 final,err:=s.LookupExportBodyFitAlignmentOrigin(ctx,parent,raw,fit,alignment);if err!=nil{return zero,err};if final!=origin{return zero,v2input.ErrInvalid};return out,ctx.Err()
}
