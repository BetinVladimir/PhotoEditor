package interchange
import (
 "context"
 "encoding/json"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

// This checks completed producer/object scope. Full native resolved schema,
// model/consent and operation semantics remain mandatory in the JSON policy.
func(s SQLExportDocumentCatalog)QualifyExportLUT(ctx context.Context,parent v2input.Entry,raw []byte,array v2input.Entry)error {
 if err:=ctx.Err();err!=nil{return err}
 if parent.Contract!="ResolvedStyle"||array.Contract!="AbsoluteLUTArray"||!validImportRef(parent.Ref)||!validImportRef(array.Ref)||len(raw)<2||len(raw)>8<<20||rawSHA(raw)!=parent.Ref.SHA256{return v2input.ErrInvalid}
 var body struct {Params struct {Image v2input.Ref `json:"image"`} `json:"params"`;LUT *struct {Artifact v2input.Ref `json:"artifact"`} `json:"absolute_lut"`}
 if json.Unmarshal(raw,&body)!=nil||body.LUT==nil||body.LUT.Artifact!=array.Ref||!validImportRef(body.Params.Image){return v2input.ErrInvalid}
 resolved,err:=s.LookupExportDocument(ctx,parent);if err!=nil{return err}
 lut,err:=s.LookupExportDocument(ctx,array);if err!=nil{return err}
 if resolved.ContentType!="application/json"||!exportArrayMediaType(lut.ContentType){return v2input.ErrInvalid}
 return tenant.WithTenant(ctx,s.DB,s.Who,s.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error {
  var allowed bool
  err:=tx.QueryRow(ctx,`SELECT EXISTS(
   SELECT 1 FROM photo.asset_versions v
   JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
   JOIN photo.artifacts source ON source.tenant_id=v.tenant_id AND source.id=v.master_artifact_id AND source.sha256=v.content_hash
   JOIN photo.artifacts resolved ON resolved.tenant_id=v.tenant_id AND resolved.id=$5 AND resolved.sha256=$6
   JOIN photo.tasks producer ON producer.tenant_id=resolved.tenant_id AND producer.id=resolved.created_by_task_id
   JOIN photo.runs origin ON origin.tenant_id=producer.tenant_id AND origin.id=producer.run_id
   LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=origin.tenant_id AND workflow.run_id=origin.id
   JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt AND attempt.fencing_token=producer.fencing_token
   JOIN photo.task_results result ON result.tenant_id=producer.tenant_id AND result.id=producer.canonical_result_id AND result.task_id=producer.id AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
   JOIN photo.artifacts lut ON lut.tenant_id=producer.tenant_id AND lut.id=$7 AND lut.sha256=$8 AND lut.created_by_task_id=producer.id
   JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id AND params.id=producer.params_artifact_id
   WHERE v.tenant_id=$1 AND asset.project_id=$2 AND v.master_artifact_id=$3 AND v.content_hash=$4
   AND v.review_state='accepted' AND asset.deleted_at IS NULL AND asset.upload_state='verified'
   AND photo.v2_editor_authorized($1,$2,$9,$10)
   AND (workflow.project_id=$2 OR origin.config_snapshot->>'project_id'=$2::text)
   AND producer.operation='style.resolve' AND producer.operation_version='1' AND producer.state='succeeded'
   AND attempt.finished_at IS NOT NULL AND attempt.backend IN('cpu','cuda','mps')
   AND attempt.backend=producer.execution_profile->>'backend' AND attempt.backend=attempt.profile->>'backend'
   AND resolved.output_role='resolved_style' AND lut.output_role='absolute_lut'
   AND resolved.produced_attempt=producer.attempt AND resolved.produced_fence=producer.fencing_token
   AND lut.produced_attempt=producer.attempt AND lut.produced_fence=producer.fencing_token
   AND producer.inputs @> jsonb_build_array(jsonb_build_object('artifact_id',$3::uuid,'sha256',$4::text))
   AND result.outputs @> jsonb_build_array(jsonb_build_object('artifact_id',$5::uuid,'sha256',$6::text),jsonb_build_object('artifact_id',$7::uuid,'sha256',$8::text))
   AND resolved.bucket=$11 AND resolved.object_key=$12 AND resolved.object_version=$13 AND resolved.bytes=$14
   AND lut.bucket=$11 AND lut.object_key=$15 AND lut.object_version=$16 AND lut.bytes=$17 AND lut.media_type=$18
   AND resolved.media_type='application/json' AND params.media_type='application/json'
   AND NOT EXISTS(SELECT 1 FROM (VALUES(source.state,source.verified_at,source.object_version,source.expires_at),
    (resolved.state,resolved.verified_at,resolved.object_version,resolved.expires_at),(lut.state,lut.verified_at,lut.object_version,lut.expires_at),
    (params.state,params.verified_at,params.object_version,params.expires_at)) o(state,verified,version,expires)
    WHERE state IS DISTINCT FROM 'ready' OR verified IS NULL OR version IS NULL OR version IN('','null') OR expires<=clock_timestamp())
  )`,s.Tenant,s.Request.ProjectID,body.Params.Image.ArtifactID,body.Params.Image.SHA256,parent.Ref.ArtifactID,parent.Ref.SHA256,array.Ref.ArtifactID,array.Ref.SHA256,
   s.Who.UserID,s.Who.SessionID,s.Bucket,resolved.Key,resolved.VersionID,resolved.Bytes,lut.Key,lut.VersionID,lut.Bytes,lut.ContentType).Scan(&allowed)
  if err!=nil{return err};if !allowed{return v2input.ErrInvalid};return ctx.Err()
 })
}
var _ ExportLUTOccurrenceAuthority=SQLExportDocumentCatalog{}
