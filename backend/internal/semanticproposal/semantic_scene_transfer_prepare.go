package semanticproposal

import (
 "context"
 "encoding/json"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

func(p TransferDraftPreparer) readCompletedSemanticSceneSource(ctx context.Context,who auth.Identity,parent detectorDraftPlan,binding v2input.SessionVersionBinding,catalogs NativeParentArtifactCatalogFactory)(v2input.SemanticSceneSource,error){
 var zero v2input.SemanticSceneSource
 if catalogs==nil{return zero,v2input.ErrInvalid}
 catalog,err:=catalogs(ctx,who,parent.request.TenantID,p.Reader.ProjectID,binding.AssetVersionID)
 if err!=nil{return zero,err}
 image,err:=p.Reader.Read(ctx,who,parent.request.TenantID,binding)
 if err!=nil{return zero,err}
 plans:=make([]semanticSceneStagePlan,2)
 results:=make([]semanticSceneStageResult,2)
 for i,operation:=range []string{"scene.parse","depth.predict"} {
  anchor,err:=compileSemanticSceneStageAnchor(ctx,who,p.Reader.ProjectID,parent.request,binding,image,operation)
  if err!=nil{return zero,err}
  saved,present,err:=p.Reader.readSemanticSceneStageSaved(ctx,who,parent.request,anchor)
  if err!=nil{return zero,err}
  if !present || !saved.Sealed || saved.TaskState!="succeeded"{return zero,v2input.ErrConflict}
  plans[i],err=p.Reader.restoreSemanticSceneStage(ctx,who,parent.request,anchor,saved,p.Configuration.Color,p.Documents,catalog)
  if err!=nil{return zero,err}
  results[i],err=p.Reader.readSemanticSceneStageResult(ctx,who,plans[i])
  if err!=nil{return zero,err}
 }
 return semanticSceneSourceFromResults(ctx,plans[0],plans[1],results[0],results[1])
}

func(p TransferDraftPreparer) prepareNativeSceneEvidence(ctx context.Context,who auth.Identity,parent detectorDraftPlan,c TransferPreparation,catalogs NativeParentArtifactCatalogFactory)([]json.RawMessage,error){
 if c.Scene==nil{return nil,nil}
 policy,err:=p.Reader.readSemanticScenePolicy(ctx,who,parent.request.TenantID,c.Qualification,c.Expected,c.Scene.Policy)
 if err!=nil{return nil,err}
 targets,err:=BindTargets(parent.request,parent.anchor.snapshotRef,parent.anchor.snapshotBody)
 if err!=nil{return nil,err}
 sources:=map[v2input.SessionVersionBinding]v2input.SemanticSceneSource{}
 bindings:=append([]v2input.SessionVersionBinding{parent.request.Intent.Reference},targets...)
 for _,binding:=range bindings {
  if _,found:=sources[binding];found{continue}
  source,err:=p.readCompletedSemanticSceneSource(ctx,who,parent,binding,catalogs)
  if err!=nil{return nil,err};sources[binding]=source
 }
 evidence:=make([]json.RawMessage,len(targets))
 for i,binding:=range targets {
  evidence[i],err=compileSemanticSceneEvidence(ctx,parent.request,parent.published.Ref,c.Qualification,policy,sources[parent.request.Intent.Reference],sources[binding])
  if err!=nil{return nil,err}
 }
 return evidence,ctx.Err()
}

// Restoring a frozen final graph never schedules new prerequisites or substitutes
// latest qualified policy. Reconstruct exact canonical source occurrences and
// recheck the physically qualified policy already embedded in each old task.
func(p TransferDraftPreparer) verifyFrozenNativeSceneEvidence(ctx context.Context,who auth.Identity,parent detectorDraftPlan,c TransferPreparation,catalogs NativeParentArtifactCatalogFactory)error{
 if len(c.sceneEvidence)==0{return ctx.Err()}
 targets,err:=BindTargets(parent.request,parent.anchor.snapshotRef,parent.anchor.snapshotBody)
 if err!=nil{return err}
 if len(targets)!=len(c.sceneEvidence){return v2input.ErrConflict}
 sources:=map[v2input.SessionVersionBinding]v2input.SemanticSceneSource{}
 for i,raw:=range c.sceneEvidence {
  evidence,err:=v2input.DecodeSemanticSceneEvidence(raw)
  if err!=nil{return err}
  policyRaw,err:=json.Marshal(evidence.Policy)
  if err!=nil{return err}
  policy,err:=p.Reader.readSemanticScenePolicy(ctx,who,parent.request.TenantID,c.Qualification,c.Expected,policyRaw)
  if err!=nil{return err}
  for _,binding:=range []v2input.SessionVersionBinding{parent.request.Intent.Reference,targets[i]} {
   if _,exists:=sources[binding];exists{continue}
   source,err:=p.readCompletedSemanticSceneSource(ctx,who,parent,binding,catalogs)
   if err!=nil{return err};sources[binding]=source
  }
  rebuilt,err:=compileSemanticSceneEvidence(ctx,parent.request,parent.published.Ref,c.Qualification,policy,sources[parent.request.Intent.Reference],sources[targets[i]])
  if err!=nil{return err}
  if !sameSemanticSceneJSON(rebuilt,raw){return v2input.ErrConflict}
 }
 return ctx.Err()
}

func sameSemanticSceneJSON(a,b json.RawMessage)bool{
 left,err:=v2input.DecodeSemanticSceneEvidence(a);if err!=nil{return false}
 right,err:=v2input.DecodeSemanticSceneEvidence(b);if err!=nil{return false}
 x,_:=json.Marshal(left);y,_:=json.Marshal(right)
 return string(x)==string(y)
}
