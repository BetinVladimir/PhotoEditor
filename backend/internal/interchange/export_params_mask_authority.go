package interchange
import (
 "context"
 "encoding/json"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)
type ExportParamsMaskAuthority interface{QualifyExportParamsMask(context.Context,v2input.Entry,[]byte,v2input.Ref)error}
func(s SQLExportDocumentCatalog)QualifyExportParamsMask(ctx context.Context,parent v2input.Entry,raw []byte,mask v2input.Ref)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.DB==nil||!validImportRef(mask){return v2input.ErrInvalid}
 masks,err:=exportParamsInlineMasks(parent,raw);if err!=nil{return err}
 found:=false;for _,body:=range masks{parsed,err:=review.ParseMaskRef(body);if err!=nil{return err};if parsed.Artifact.ID==mask.ArtifactID&&parsed.Artifact.SHA256==mask.SHA256{found=true}}
 if !found{return v2input.ErrInvalid}
 var fields map[string]json.RawMessage;if json.Unmarshal(raw,&fields)!=nil{return v2input.ErrInvalid}
 operation,err:=exportParamsOperationShape(raw);if err!=nil||operation==""{return v2input.ErrInvalid}
 var source v2input.Ref;if json.Unmarshal(fields["image"],&source)!=nil||!validImportRef(source){return v2input.ErrInvalid}
 object,err:=s.LookupExportDocument(ctx,parent);if err!=nil{return err}
 return tenant.WithTenant(ctx,s.DB,s.Who,s.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var allowed bool
  err:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM photo.tasks producer
   JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id AND params.id=producer.params_artifact_id
   JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
   JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt AND attempt.fencing_token=producer.fencing_token
   JOIN photo.runs origin ON origin.tenant_id=producer.tenant_id AND origin.id=producer.run_id
   LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=origin.tenant_id AND workflow.run_id=origin.id
   WHERE producer.tenant_id=$1 AND params.id=$2 AND params.sha256=$3 AND producer.operation=$4 AND producer.operation_version='1' AND producer.state='succeeded'
   AND attempt.finished_at IS NOT NULL AND attempt.backend=producer.execution_profile->>'backend' AND attempt.backend=attempt.profile->>'backend'
   AND NOT origin.cancel_requested AND (workflow.project_id=$5 OR origin.config_snapshot->>'project_id'=$5::text)
   AND photo.v2_editor_authorized($1,$5,$6,$7)
   AND producer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',$8::uuid,'sha256',$9::text),jsonb_build_object('artifact_id',$10::uuid,'sha256',$11::text))
   AND params.bucket=$12 AND params.object_key=$13 AND params.object_version=$14 AND params.bytes=$15 AND params.media_type='application/json'
   AND params.state='ready' AND params.verified_at IS NOT NULL AND (params.expires_at IS NULL OR params.expires_at>clock_timestamp())
   AND EXISTS(SELECT 1 FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
    WHERE v.tenant_id=$1 AND a.project_id=$5 AND v.master_artifact_id=$8 AND v.content_hash=$9 AND v.review_state='accepted' AND a.deleted_at IS NULL AND a.upload_state='verified'))`,
   s.Tenant,parent.Ref.ArtifactID,parent.Ref.SHA256,operation,s.Request.ProjectID,s.Who.UserID,s.Who.SessionID,source.ArtifactID,source.SHA256,mask.ArtifactID,mask.SHA256,s.Bucket,object.Key,object.VersionID,object.Bytes).Scan(&allowed)
  if err!=nil{return err};if !allowed{return v2input.ErrInvalid};return ctx.Err()
 })
}
var _ ExportParamsMaskAuthority=SQLExportDocumentCatalog{}
