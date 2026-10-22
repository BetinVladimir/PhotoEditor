package semanticproposal

import (
 "context"
 "encoding/json"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func(s SQLTransferSourceReader) registerSemanticSceneStage(ctx context.Context,tx pgx.Tx,who auth.Identity,parent detectorDraftPlan,plan semanticSceneStagePlan,built v2input.Built)error{
 if err:=verifySemanticSceneStagePlan(ctx,plan);err!=nil{return err}
 transport,err:=plan.prepared.frozen.BindPublishedTransport(ctx,built)
 if err!=nil{return err}
 binding,err:=marshalNativeRegistrationBinding(ctx,plan.prepared.frozen,built,plan.prepared.published.anchor.operation)
 if err!=nil{return err}
 if _,err:=s.beginSemanticSceneStage(ctx,tx,who,parent,plan);err!=nil{return err}
 state:=func()(string,error){
  if err:=ctx.Err();err!=nil{return "",err}
  var current string
  err:=tx.QueryRow(ctx,"SELECT photo.semantic_scene_registration_binding_state($1,$2,$3::jsonb)",plan.shell.Tenant,plan.shell.Run,string(binding)).Scan(&current)
  return current,err
 }
 current,err:=state()
 if err!=nil{return err}
 if current=="replayed"{return ctx.Err()}
 if current!="missing"{return v2input.ErrConflict}
 if err:=v2input.RegisterInTransaction(ctx,tx,plan.shell.Tenant,plan.shell.Run,transport.Graph.ID,built,transport.Graph.Open,transport.Graph.Direct);err!=nil{return err}
 current,err=state()
 if err!=nil{return err}
 if current!="replayed"{return v2input.ErrConflict}
 return ctx.Err()
}

func(s SQLTransferSourceReader) sealSemanticSceneStage(ctx context.Context,tx pgx.Tx,who auth.Identity,parent detectorDraftPlan,plan semanticSceneStagePlan,built v2input.Built)(v2input.JobTicket,error){
 var zero v2input.JobTicket
 if err:=verifySemanticSceneStagePlan(ctx,plan);err!=nil{return zero,err}
 // This repeats complete parent/source/Params/report authority under the same
 // transaction after potentially long object publication.
 if _,err:=s.beginSemanticSceneStage(ctx,tx,who,parent,plan);err!=nil{return zero,err}
 binding,err:=marshalNativeRegistrationBinding(ctx,plan.prepared.frozen,built,plan.prepared.published.anchor.operation)
 if err!=nil{return zero,err}
 packet,err:=canonical.Marshal(map[string]any{"schema_version":1,"tenant_id":plan.shell.Tenant,"project_id":s.ProjectID,"actor_id":who.UserID,"session_id":who.SessionID,"run_id":plan.shell.Run,"request_sha":parent.request.RequestSHA,"config_text":string(plan.shell.Config),"policy_text":plan.prepared.published.qualification.expected,"registration":json.RawMessage(binding)})
 if err!=nil{return zero,err}
 return sealNativePreparationQueryInTransaction(ctx,tx,plan.shell.Tenant,plan.shell.Run,plan.shell.WorkflowID,packet,"SELECT photo.seal_semantic_scene_preparation($1,$2,$3::jsonb)")
}
