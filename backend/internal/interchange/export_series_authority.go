package interchange

import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

type ExportSeriesAuthority interface{QualifyExportSeries(context.Context,v2input.Entry,[]byte)error}

// Metadata proof supplements full native schema and original series input
// semantic checks. A syntactically valid file is never sufficient authority.
func(s SQLExportDocumentCatalog)QualifyExportSeries(ctx context.Context,entry v2input.Entry,raw []byte)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.DB==nil||entry.Contract!="SeriesCorrections"||!validImportRef(entry.Ref)||len(raw)<2||len(raw)>8<<20||rawSHA(raw)!=entry.Ref.SHA256{return v2input.ErrInvalid}
 object,err:=s.LookupExportDocument(ctx,entry);if err!=nil{return err}
 err=tenant.WithTenant(ctx,s.DB,s.Who,s.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var allowed bool
  err:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM photo.artifacts series
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
    WHERE a.state IS DISTINCT FROM 'ready' OR a.verified_at IS NULL OR a.object_version IS NULL OR a.object_version IN('','null') OR (a.expires_at IS NOT NULL AND a.expires_at<=clock_timestamp())))`,s.Tenant,entry.Ref.ArtifactID,entry.Ref.SHA256,s.Request.ProjectID,s.Who.UserID,s.Who.SessionID,s.Bucket,object.Key,object.VersionID,object.Bytes).Scan(&allowed)
  if err!=nil{return err};if !allowed{return v2input.ErrInvalid};return ctx.Err()
 })
 if err!=nil{return err}
 current,err:=s.LookupExportDocument(ctx,entry);if err!=nil{return err};if current!=object{return v2input.ErrInvalid};return ctx.Err()
}

type ExportSeriesProducerValidator struct{Authority ExportSeriesAuthority;Other ExportTypedDocumentValidator}
func(s ExportSeriesProducerValidator)ExportDocumentChildren(ctx context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if s.Authority==nil||s.Other==nil{return nil,v2input.ErrInvalid}
 body:=append([]byte(nil),raw...)
 if entry.Contract=="SeriesCorrections"{if err:=s.Authority.QualifyExportSeries(ctx,entry,append([]byte(nil),body...));err!=nil{return nil,err}}
 children,err:=s.Other.ExportDocumentChildren(ctx,entry,append([]byte(nil),body...));if err!=nil{return nil,err}
 if entry.Contract=="SeriesCorrections"{
  if len(children)!=0{return nil,v2input.ErrInvalid}
  if err:=s.Authority.QualifyExportSeries(ctx,entry,append([]byte(nil),body...));err!=nil{return nil,err}
 }
 return append([]v2input.Entry(nil),children...),ctx.Err()
}
var _ ExportSeriesAuthority=SQLExportDocumentCatalog{}
var _ ExportTypedDocumentValidator=ExportSeriesProducerValidator{}
