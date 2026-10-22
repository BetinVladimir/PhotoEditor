package semanticproposal

import (
 "context"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func verifySemanticSceneStagePlan(ctx context.Context,plan semanticSceneStagePlan)error{
 if err:=ctx.Err();err!=nil{return err}
 p:=plan.prepared
 if plan.seal=="" || len(plan.shell.Tasks)!=1 || len(plan.shell.Edges)!=0 || plan.shell.Run!=p.published.anchor.run || plan.shell.WorkflowID!=p.published.anchor.workflow || plan.key!="scene-prep:"+plan.shell.Run.String(){return v2input.ErrInvalid}
 task,err:=p.frozen.DraftTask()
 if err!=nil{return err}
 sha,err:=canonical.Hash(task)
 if err!=nil || sha!=p.taskSHA{return v2input.ErrConflict}
 declaredSHA,err:=canonical.Hash(plan.shell.Tasks[0])
 if err!=nil || sha!=declaredSHA{return v2input.ErrConflict}
 seal,err:=canonical.Hash(map[string]any{"shell":plan.shell,"key":plan.key,"source_body":p.published.anchor.sourceBody,"params_body":p.published.body})
 if err!=nil || seal!=plan.seal{return v2input.ErrConflict}
 return ctx.Err()
}

// Only the committed blocked draft may be published by the orchestrator. This
// proof binds the actual manifest/streams to the frozen task; it never grants
// queue activation. Registration and final seal repeat current SQL authority.
func publishSemanticSceneStageTransport(ctx context.Context,plan semanticSceneStagePlan,publisher v2input.Publisher)(v2input.PreparedSemanticTask,defectRegistrationProof,error){
 var zero v2input.PreparedSemanticTask
 var noProof defectRegistrationProof
 if err:=verifySemanticSceneStagePlan(ctx,plan);err!=nil{return zero,noProof,err}
 if publisher==nil{return zero,noProof,v2input.ErrInvalid}
 prepared,err:=v2input.PublishSemanticAnalysisTask(ctx,plan.prepared.frozen,publisher)
 if err!=nil{return zero,noProof,err}
 task:=plan.shell.Tasks[0]
 if prepared.Graph.ID!=task.ID || prepared.Params.ArtifactID!=task.ParamsArtifactID || prepared.Params.SHA256!=task.ParamsSHA || prepared.InputDigest!=task.InputDigest{return zero,noProof,v2input.ErrConflict}
 proof,err:=freezeDefectRegistrationProof(ctx,prepared.Graph)
 if err!=nil{return zero,noProof,err}
 if err:=verifySemanticSceneStagePlan(ctx,plan);err!=nil{return zero,noProof,err}
 return prepared,proof,nil
}
