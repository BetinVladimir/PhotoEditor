package semanticproposal

import (
 "context"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

// Called under the original parent lock after its ordinary source/target
// protection pipeline. Exact repeated bindings reuse one deterministic child
// pair; an analysis for another parent or version can never satisfy this stage.
func(p TransferDraftPreparer) advanceNativeScenePrerequisites(ctx context.Context,who auth.Identity,parent detectorDraftPlan,manifest nativeTargetPreparationManifest,catalogs NativeParentArtifactCatalogFactory)(nativeFacePipelineAdvance,error){
 ready:=nativeFacePipelineAdvance{phase:nativePreparationWaiting,operation:"semantic.transfer"}
 frozen,err:=frozenSemanticSceneParentPolicy(ctx,parent)
 if err!=nil{return nativeFacePipelineAdvance{},err}
 p.Configuration.Scene=frozen
 if frozen==nil{return ready,nil} // historical accepted path
 if catalogs==nil{return nativeFacePipelineAdvance{},v2input.ErrInvalid}
 if err:=verifyDetectorPlanSeal(ctx,parent);err!=nil{return nativeFacePipelineAdvance{},err}
 if _,err:=restoreNativeTargetPreparationManifest(ctx,who,parent.request,parent.requestKey,parent.published,parent.anchor.snapshotRef,parent.anchor.snapshotBody,manifest.body,manifest.sha);err!=nil{return nativeFacePipelineAdvance{},err}
 targets,err:=BindTargets(parent.request,parent.anchor.snapshotRef,parent.anchor.snapshotBody)
 if err!=nil{return nativeFacePipelineAdvance{},err}
 bindings:=append([]v2input.SessionVersionBinding{parent.request.Intent.Reference},targets...)
 seen:=map[v2input.SessionVersionBinding]bool{}
 for _,binding:=range bindings {
  if seen[binding]{continue};seen[binding]=true
  catalog,err:=catalogs(ctx,who,parent.request.TenantID,p.Reader.ProjectID,binding.AssetVersionID)
  if err!=nil{return nativeFacePipelineAdvance{},err}
  for _,operation:=range []string{"scene.parse","depth.predict"} {
   stage,err:=p.advanceSemanticSceneStage(ctx,who,parent,binding,catalog,operation)
   if err!=nil{return nativeFacePipelineAdvance{},err}
   if stage.phase!=nativePreparationSucceeded{return nativeFacePipelineAdvance{phase:stage.phase,operation:operation},nil}
  }
 }
 return ready,ctx.Err()
}
