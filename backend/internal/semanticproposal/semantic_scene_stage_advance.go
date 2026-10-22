package semanticproposal

import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

type semanticSceneStageAdvance struct {
 phase nativeInputPreparationPhase
 operation string
 plan semanticSceneStagePlan
 result semanticSceneStageResult
}

// The original parent's advisory lock encloses this whole bounded advancement.
// Object publication stays outside SQL transactions. A committed child is
// always restored from its own immutable marker, including costs/runtimepins.
func(p TransferDraftPreparer) advanceSemanticSceneStage(ctx context.Context,who auth.Identity,parent detectorDraftPlan,binding v2input.SessionVersionBinding,catalog v2draft.ArtifactCatalog,operation string)(semanticSceneStageAdvance,error){
 var zero semanticSceneStageAdvance
 if p.Configuration.Scene==nil || p.Scheduler==nil || p.TransportDocuments==nil{return zero,v2input.ErrInvalid}
 image,err:=p.Reader.Read(ctx,who,parent.request.TenantID,binding)
 if err!=nil{return zero,err}
 anchor,err:=compileSemanticSceneStageAnchor(ctx,who,p.Reader.ProjectID,parent.request,binding,image,operation)
 if err!=nil{return zero,err}
 saved,present,err:=p.Reader.readSemanticSceneStageSaved(ctx,who,parent.request,anchor)
 if err!=nil{return zero,err}
 if present {
  phase,err:=nativeInputStagePreparationPhase(nativeInputSavedStageState{task:anchor.task,taskState:saved.TaskState,runState:saved.RunState,sealed:saved.Sealed})
  if err!=nil{return zero,err}
  if phase==nativePreparationFailed{return semanticSceneStageAdvance{phase:phase,operation:operation},nil}
 }
 var plan semanticSceneStagePlan
 if present {
  plan,err=p.Reader.restoreSemanticSceneStage(ctx,who,parent.request,anchor,saved,p.Configuration.Color,p.Documents,catalog)
  if err!=nil{return zero,err}
 } else {
  producer:=p.Configuration.Scene.Scene
  if operation=="depth.predict"{producer=p.Configuration.Scene.Depth}
  published,err:=p.Reader.publishSemanticSceneStageParams(ctx,who,parent.request,anchor,p.Configuration.Color,p.Documents,producer)
  if err!=nil{return zero,err}
  budget:=nativePreparationBudget{Costs:producer.Costs,RAM:p.Configuration.RAM,VRAM:p.Configuration.VRAM,Scratch:p.Configuration.Scratch,Output:p.Configuration.Output,Reserved:p.ReservedBytes}
  task,err:=p.Reader.prepareSemanticSceneStageTask(ctx,who,parent.request,published,catalog,producer,budget)
  if err!=nil{return zero,err}
  plan,err=p.Reader.compileSemanticSceneStagePlan(ctx,who,parent.request,task,budget.Reserved)
  if err!=nil{return zero,err}
  if err:=database.InTransaction(ctx,p.Scheduler,func(tx pgx.Tx)error{_,err:=p.Reader.beginSemanticSceneStage(ctx,tx,who,parent,plan);return err});err!=nil{return zero,err}
 }
 waiting:=semanticSceneStageAdvance{phase:nativePreparationWaiting,operation:operation,plan:plan}
 if !present || !saved.Sealed {
  transport,_,err:=publishSemanticSceneStageTransport(ctx,plan,v2input.ScopedPublisher{Documents:p.TransportDocuments,Tenant:parent.request.TenantID,Run:plan.shell.Run})
  if err!=nil{return zero,err}
  if err:=database.InTransaction(ctx,p.Scheduler,func(tx pgx.Tx)error{
   if err:=p.Reader.registerSemanticSceneStage(ctx,tx,who,parent,plan,transport.Graph.Built);err!=nil{return err}
   _,err:=p.Reader.sealSemanticSceneStage(ctx,tx,who,parent,plan,transport.Graph.Built)
   return err
  });err!=nil{return zero,err}
  return waiting,nil
 }
 phase,err:=nativeInputStagePreparationPhase(nativeInputSavedStageState{task:anchor.task,taskState:saved.TaskState,runState:saved.RunState,sealed:saved.Sealed})
 if err!=nil{return zero,err}
 if phase!=nativePreparationSucceeded{waiting.phase=phase;return waiting,nil}
 result,err:=p.Reader.readSemanticSceneStageResult(ctx,who,plan)
 if err!=nil{return zero,err}
 if err:=database.InTransaction(ctx,p.Scheduler,func(tx pgx.Tx)error{
  if _,err:=p.Reader.beginSemanticSceneStage(ctx,tx,who,parent,plan);err!=nil{return err}
  var ready bool
  v:=result.value
  if err:=tx.QueryRow(ctx,"SELECT photo.finish_semantic_scene_preparation($1,$2,$3,$4,$5,$6)",parent.request.TenantID,plan.shell.Run,v.Task,v.Result,v.Attempt,v.Fence).Scan(&ready);err!=nil{return err}
  if !ready{return v2input.ErrConflict};return ctx.Err()
 });err!=nil{return zero,err}
 return semanticSceneStageAdvance{phase:nativePreparationSucceeded,operation:operation,plan:plan,result:result},ctx.Err()
}
