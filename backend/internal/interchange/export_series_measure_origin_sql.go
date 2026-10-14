package interchange

import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

func(s SQLExportDocumentCatalog)LookupExportSeriesMeasureOrigin(ctx context.Context,balance ExportSeriesOriginEvidence,entry v2input.Entry,raw []byte)(ExportSeriesMeasureOrigin,error){
 var zero,out ExportSeriesMeasureOrigin
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.DB==nil||entry.Contract!="SeriesStats"||!validImportRef(entry.Ref)||len(raw)<2||len(raw)>1<<20||rawSHA(raw)!=entry.Ref.SHA256{return zero,v2input.ErrInvalid}
 body:=append([]byte(nil),raw...)
 if err:=s.QualifyExportSeriesStatImage(ctx,balance,entry,body);err!=nil{return zero,err}
 stats,err:=s.LookupExportSeriesOriginDocument(ctx,balance,entry);if err!=nil{return zero,err}
 err=tenant.WithTenant(ctx,s.DB,s.Who,s.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var bucket string
  err:=tx.QueryRow(ctx,`SELECT measure.id,measure.attempt,measure.fencing_token,params.id,params.sha256,params.bucket,params.object_key,params.object_version,params.sha256,params.bytes,params.media_type
   FROM photo.artifacts stats
   JOIN photo.tasks measure ON measure.tenant_id=stats.tenant_id AND measure.id=stats.created_by_task_id
   JOIN photo.artifacts params ON params.tenant_id=measure.tenant_id AND params.id=measure.params_artifact_id
   JOIN photo.task_results result ON result.tenant_id=measure.tenant_id AND result.id=measure.canonical_result_id AND result.task_id=measure.id AND result.attempt=measure.attempt AND result.fencing_token=measure.fencing_token
   JOIN photo.task_attempts attempt ON attempt.tenant_id=measure.tenant_id AND attempt.task_id=measure.id AND attempt.attempt=measure.attempt AND attempt.fencing_token=measure.fencing_token
   JOIN photo.runs run ON run.tenant_id=measure.tenant_id AND run.id=measure.run_id
   LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=run.tenant_id AND workflow.run_id=run.id
   WHERE stats.tenant_id=$1 AND stats.id=$2 AND stats.sha256=$3 AND stats.output_role='series_stats'
   AND stats.produced_attempt=measure.attempt AND stats.produced_fence=measure.fencing_token
   AND stats.bucket=$4 AND stats.object_key=$5 AND stats.object_version=$6 AND stats.bytes=$7
   AND measure.operation='series.measure' AND measure.operation_version='1' AND measure.state='succeeded' AND measure.resource_class='cpu'
   AND attempt.finished_at IS NOT NULL AND attempt.backend='cpu' AND attempt.profile->>'backend'='cpu' AND measure.execution_profile->>'backend'='cpu'
   AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',$2::uuid,'sha256',$3::text))
   AND NOT run.cancel_requested AND (workflow.project_id=$8 OR run.config_snapshot->>'project_id'=$8::text)
   AND photo.v2_editor_authorized($1,$8,$9,$10)
   AND params.bucket=$4 AND params.media_type='application/json' AND params.bytes BETWEEN 2 AND 1048576
   AND params.state='ready' AND params.verified_at IS NOT NULL AND params.object_version IS NOT NULL AND params.object_version NOT IN('','null')
   AND (params.expires_at IS NULL OR params.expires_at>clock_timestamp())`,s.Tenant,entry.Ref.ArtifactID,entry.Ref.SHA256,s.Bucket,stats.Key,stats.VersionID,stats.Bytes,s.Request.ProjectID,s.Who.UserID,s.Who.SessionID).Scan(&out.Task,&out.Attempt,&out.Fence,&out.Params.ArtifactID,&out.Params.SHA256,&bucket,&out.Object.Key,&out.Object.VersionID,&out.Object.SHA256,&out.Object.Bytes,&out.Object.ContentType)
  if err!=nil{return err};if bucket!=s.Bucket||!validImportRef(out.Params)||out.Object.SHA256!=out.Params.SHA256||out.Object.Key==""{return v2input.ErrInvalid};return ctx.Err()
 })
 if err!=nil{return zero,err}
 if err:=s.QualifyExportSeriesStatImage(ctx,balance,entry,body);err!=nil{return zero,err}
 current,err:=s.LookupExportSeriesOriginDocument(ctx,balance,entry);if err!=nil{return zero,err};if current!=stats{return zero,v2input.ErrInvalid};return out,ctx.Err()
}
var _ ExportSeriesMeasureOriginSource=SQLExportDocumentCatalog{}
