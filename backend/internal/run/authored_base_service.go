package run

import (
 "context"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/project"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

// AuthoredBaseRequest carries the exact immutable parent expectation. It does
// not contain a ready receipt, qualified callback, or mutable replacement style.
// SQL parent-origin authority must already exist; Begin cannot create it.
type AuthoredBaseRequest struct {
 Intent EditorEffectRequest
 ParentRunID uuid.UUID
 ProgramSHA string
 PhasePlanSHA string
 EffectiveStyle v2input.Ref
}
func(s EditorEffectsService)BeginAuthoredBase(ctx context.Context,who auth.Identity,tid,pid uuid.UUID,key string,req AuthoredBaseRequest)(idempotency.Response,error){
 if req.ParentRunID==uuid.Nil||!isLowerHexSHA(req.ProgramSHA)||!isLowerHexSHA(req.PhasePlanSHA)||req.EffectiveStyle.ArtifactID==uuid.Nil||!isLowerHexSHA(req.EffectiveStyle.SHA256){return idempotency.Response{},domain.NewError("VALIDATION_FAILED",422)}
 origin:=authoredBaseApplyBinding{ParentRunID:req.ParentRunID,ProgramSHA:req.ProgramSHA,PhasePlanSHA:req.PhasePlanSHA,OriginalSource:req.Intent.Source,EffectiveStyle:req.EffectiveStyle}
 // The existing guarded loader recompiles all style/session/learning/override
 // inputs against the original freeze before publishing the actual style.
 return s.begin(ctx,who,tid,pid,key,req.Intent,&origin)
}
func(s EditorEffectsService)replayAuthoredBase(ctx context.Context,who auth.Identity,tid,pid uuid.UUID,key,route string,req EditorEffectRequest,origin authoredBaseApplyBinding)(idempotency.Response,bool,error){
 var out idempotency.Response;found:=false
 fingerprint:=struct{Intent EditorEffectRequest `json:"intent"`;Origin authoredBaseApplyBinding `json:"authored_base"`}{req,origin}
 hash,err:=canonical.Hash(fingerprint);if err!=nil{return out,false,err}
 sourceRaw,err:=canonical.Marshal(req.Source);if err!=nil{return out,false,err}
 styleRaw,err:=canonical.Marshal(origin.EffectiveStyle);if err!=nil{return out,false,err}
 err=tenant.WithTenant(ctx,s.Runs.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  if _,e:=project.Authorize(ctx,tx,tid,pid,who.UserID,role,domain.Editor,false);e!=nil{return e}
  var stored string;var status *int;var body []byte;var child *uuid.UUID
  e:=tx.QueryRow(ctx,`SELECT request_hash,response_status,response_body,resource_id FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route=$3 AND key=$4`,tid,who.UserID,route,key).Scan(&stored,&status,&body,&child)
  if errors.Is(e,pgx.ErrNoRows){return nil};if e!=nil{return e}
  if stored!=hash{return domain.NewError("IDEMPOTENCY_CONFLICT",409)};if status==nil{return nil}
  if child==nil||!validEditorEffectReceipt(*status,body,*child){return domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
  // Accepted recovery authenticates immutable relationship and current reader;
  // it does not require a new installation, live worker, active parent lease,
  // or re-entry into fresh-admission scope after the parent has committed.
  var valid bool
  e=tx.QueryRow(ctx,`SELECT EXISTS(SELECT FROM photo.authored_render_base_children b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.child_run_id JOIN photo.authored_render_intents i ON i.tenant_id=b.tenant_id AND i.parent_run_id=b.parent_run_id WHERE b.tenant_id=$1 AND b.parent_run_id=$2 AND b.child_run_id=$3 AND b.project_id=$4 AND b.actor_id=$5 AND b.program_sha=$6 AND b.phase_plan_sha=$7 AND i.original_recipe_id=$8 AND i.original_recipe_sha=$9 AND r.kind='apply' AND r.created_by=$5 AND r.output_collection_id IS NULL AND r.config_snapshot#>'{authored_render_origin,original_source}'=$10::jsonb AND r.config_snapshot#>'{authored_render_origin,effective_style}'=$11::jsonb)`,tid,origin.ParentRunID,*child,pid,who.UserID,origin.ProgramSHA,origin.PhasePlanSHA,req.Recipe.ArtifactID,req.Recipe.SHA256,string(sourceRaw),string(styleRaw)).Scan(&valid)
  if e!=nil{return e};if !valid{return domain.NewError("SOURCE_VERSION_MISMATCH",409)}
  out=idempotency.Response{Status:*status,Body:body,ResourceID:child,Replayed:true};found=true;return nil
 })
 return out,found,err
}
