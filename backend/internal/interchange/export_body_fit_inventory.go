package interchange

import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

type ExportBodyFitInputInventory struct{Inputs []v2input.Ref;Before,After v2input.Ref}
func exportBodyFitInputInventory(node ExportClosureNode)(ExportBodyFitInputInventory,error){
 var zero ExportBodyFitInputInventory
 if node.Entry.Contract!="BodyFit"||!validImportRef(node.Entry.Ref)||len(node.Body)<2||len(node.Body)>2<<20||rawSHA(node.Body)!=node.Entry.Ref.SHA256{return zero,v2input.ErrInvalid}
 refs,err:=v2draft.DocumentChildren(node.Body,"BodyFit",node.Entry.Ref);if err!=nil{return zero,err}
 result:=ExportBodyFitInputInventory{Inputs:[]v2input.Ref{}}
 for _,ref:=range refs{switch ref.Path{
 case "Document.params.before":result.Before=ref.Ref;result.Inputs=append(result.Inputs,ref.Ref)
 case "Document.params.after":result.After=ref.Ref;result.Inputs=append(result.Inputs,ref.Ref)
 case "Document.params.alignment","Document.params.before_pose","Document.params.after_pose","Document.params.before_person.artifact","Document.params.after_person.artifact","Document.params.before_faces","Document.params.after_faces":result.Inputs=append(result.Inputs,ref.Ref)
 }}
 if !validImportRef(result.Before)||!validImportRef(result.After)||(len(result.Inputs)!=7&&len(result.Inputs)!=9){return zero,v2input.ErrInvalid}
 return result,nil
}

// Membership and accepted paired sources only; native original Params/result,
// actual images/alignment/pose/mask content and current model policy remain gates.
func(s SQLExportDocumentCatalog)QualifyExportBodyFitInputInventory(ctx context.Context,parent v2input.Entry,raw []byte,fit ExportClosureNode)error{
 if err:=ctx.Err();err!=nil{return err};if s.DB==nil{return v2input.ErrInvalid}
 inventory,err:=exportBodyFitInputInventory(fit);if err!=nil{return err}
 body:=append([]byte(nil),raw...)
 origin,err:=s.LookupExportBodyDependencyOrigin(ctx,parent,body,fit.Entry);if err!=nil{return err}
 if origin.Producer.Operation!="pair.fit.body"||origin.Output.Bytes!=int64(len(fit.Body)){return v2input.ErrInvalid}
 inputs,err:=canonical.Marshal(inventory.Inputs);if err!=nil{return err}
 err=tenant.WithTenant(ctx,s.DB,s.Who,s.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var allowed bool
  err:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM photo.tasks producer
   JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt AND attempt.fencing_token=producer.fencing_token
   WHERE producer.tenant_id=$1 AND producer.id=$2 AND producer.attempt=$3 AND producer.fencing_token=$4
   AND producer.operation='pair.fit.body' AND producer.operation_version='1' AND producer.state='succeeded' AND producer.resource_class='cpu'
   AND producer.inputs @> $5::jsonb AND producer.params_artifact_id=$6
   AND attempt.finished_at IS NOT NULL AND attempt.backend='cpu' AND attempt.profile->>'backend'='cpu' AND producer.execution_profile->>'backend'='cpu'
   AND NOT EXISTS(SELECT 1 FROM (VALUES($7::uuid,$8::text),($9::uuid,$10::text)) required(id,sha)
    WHERE NOT EXISTS(SELECT 1 FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
     JOIN photo.artifacts master ON master.tenant_id=v.tenant_id AND master.id=v.master_artifact_id AND master.sha256=v.content_hash
     WHERE v.tenant_id=$1 AND a.project_id=$11 AND v.master_artifact_id=required.id AND v.content_hash=required.sha
     AND v.review_state='accepted' AND a.deleted_at IS NULL AND a.upload_state='verified'
     AND master.state='ready' AND master.verified_at IS NOT NULL AND master.object_version IS NOT NULL AND master.object_version NOT IN('','null')
     AND (master.expires_at IS NULL OR master.expires_at>clock_timestamp())))
   AND photo.v2_editor_authorized($1,$11,$12,$13))`,s.Tenant,origin.Producer.Task,origin.Producer.Attempt,origin.Producer.Fence,inputs,origin.Params.ArtifactID,inventory.Before.ArtifactID,inventory.Before.SHA256,inventory.After.ArtifactID,inventory.After.SHA256,s.Request.ProjectID,s.Who.UserID,s.Who.SessionID).Scan(&allowed)
  if err!=nil{return err};if !allowed{return v2input.ErrInvalid};return ctx.Err()
 });if err!=nil{return err}
 final,err:=s.LookupExportBodyDependencyOrigin(ctx,parent,body,fit.Entry);if err!=nil{return err};if final!=origin{return v2input.ErrInvalid};return ctx.Err()
}
