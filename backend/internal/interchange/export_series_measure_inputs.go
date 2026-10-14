package interchange

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

type ExportSeriesMeasureInputAuthority interface{QualifyExportSeriesMeasureInputs(context.Context,ExportSeriesMeasureOrigin,v2input.Entry,[]byte,ExportClosureNode)error}
func exportSeriesMeasureInputRefs(origin ExportSeriesMeasureOrigin,raw []byte,image ExportClosureNode)([]v2input.Ref,error){
 if !validImportRef(origin.Params)||len(raw)<2||len(raw)>1<<20||rawSHA(raw)!=origin.Params.SHA256||image.Entry.Contract!="WorkingImage"||!validImportRef(image.Entry.Ref)||rawSHA(image.Body)!=image.Entry.Ref.SHA256{return nil,v2input.ErrInvalid}
 var params struct{Image v2input.Ref `json:"image"`;Regions []struct{Mask json.RawMessage `json:"mask"`} `json:"regions"`}
 if json.Unmarshal(raw,&params)!=nil||params.Image!=image.Entry.Ref||len(params.Regions)<1||len(params.Regions)>32{return nil,v2input.ErrInvalid}
 working,err:=review.ParseWorkingImage(image.Body);if err!=nil{return nil,err}
 chain,err:=canonical.Bytes(working.TransformChain);if err!=nil{return nil,err}
 refs:=[]v2input.Ref{params.Image,{ArtifactID:working.Pixels.ID,SHA256:working.Pixels.SHA256}}
 if working.Alpha!=nil{refs=append(refs,v2input.Ref{ArtifactID:working.Alpha.ID,SHA256:working.Alpha.SHA256})}
 for _,region:=range params.Regions{
  mask,err:=review.ParseMaskRef(region.Mask);if err!=nil{return nil,err};maskChain,err:=canonical.Bytes(mask.TransformChain);if err!=nil{return nil,err}
  if mask.Protected||mask.Canvas!=working.Canvas||!bytes.Equal(chain,maskChain){return nil,v2input.ErrInvalid}
  ref:=v2input.Ref{ArtifactID:mask.Artifact.ID,SHA256:mask.Artifact.SHA256};if !validImportRef(ref){return nil,v2input.ErrInvalid};refs=append(refs,ref)
 }
 return refs,nil
}

func(s SQLExportDocumentCatalog)QualifyExportSeriesMeasureInputs(ctx context.Context,origin ExportSeriesMeasureOrigin,stat v2input.Entry,raw []byte,image ExportClosureNode)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.DB==nil||stat.Contract!="SeriesStats"||!validImportRef(stat.Ref)||origin.Attempt<1||origin.Fence<1{return v2input.ErrInvalid}
 refs,err:=exportSeriesMeasureInputRefs(origin,raw,image);if err!=nil{return err}
 inputs,err:=canonical.Marshal(refs);if err!=nil{return err}
 return tenant.WithTenant(ctx,s.DB,s.Who,s.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var allowed bool
  err:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM photo.tasks measure
   JOIN photo.artifacts params ON params.tenant_id=measure.tenant_id AND params.id=measure.params_artifact_id
   JOIN photo.artifacts stats ON stats.tenant_id=measure.tenant_id AND stats.created_by_task_id=measure.id AND stats.id=$5 AND stats.sha256=$6
   JOIN photo.task_results result ON result.tenant_id=measure.tenant_id AND result.id=measure.canonical_result_id AND result.task_id=measure.id AND result.attempt=measure.attempt AND result.fencing_token=measure.fencing_token
   JOIN photo.runs run ON run.tenant_id=measure.tenant_id AND run.id=measure.run_id
   LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=run.tenant_id AND workflow.run_id=run.id
   WHERE measure.tenant_id=$1 AND measure.id=$2 AND measure.attempt=$3 AND measure.fencing_token=$4
   AND measure.operation='series.measure' AND measure.operation_version='1' AND measure.state='succeeded' AND measure.inputs=$7::jsonb
   AND stats.output_role='series_stats' AND stats.produced_attempt=measure.attempt AND stats.produced_fence=measure.fencing_token
   AND params.id=$8 AND params.sha256=$9 AND params.bucket=$10 AND params.object_key=$11 AND params.object_version=$12 AND params.bytes=$13
   AND params.state='ready' AND params.verified_at IS NOT NULL AND params.media_type='application/json' AND (params.expires_at IS NULL OR params.expires_at>clock_timestamp())
   AND NOT run.cancel_requested AND (workflow.project_id=$14 OR run.config_snapshot->>'project_id'=$14::text)
   AND photo.v2_editor_authorized($1,$14,$15,$16))`,s.Tenant,origin.Task,origin.Attempt,origin.Fence,stat.Ref.ArtifactID,stat.Ref.SHA256,inputs,origin.Params.ArtifactID,origin.Params.SHA256,s.Bucket,origin.Object.Key,origin.Object.VersionID,origin.Object.Bytes,s.Request.ProjectID,s.Who.UserID,s.Who.SessionID).Scan(&allowed)
  if err!=nil{return err};if !allowed{return v2input.ErrInvalid};return ctx.Err()
 })
}
var _ ExportSeriesMeasureInputAuthority=SQLExportDocumentCatalog{}
