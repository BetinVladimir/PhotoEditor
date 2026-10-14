package v2optical

import (
 "context"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

// sourceDocument resolves only original bytes owned by the ready-step physical
// source proofs. Missing unchanged inputs need their own verified proof; a
// caller-supplied WorkingImage document is never silently substituted here.
func(p PreparedMappedReplacementStep)sourceDocument(ctx context.Context,ref v2input.Ref)([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 parents:=p.parents;l:=parents.step.intent.locator;who:=parents.step.intent.who
 selected:=parents.selected.selection.SelectedBase()
 if selected!=nil&&*selected==ref{
  if _,_,err:=selectedNegativeProofPacket(selectedSourceBucket(parents.selected.selection),who,l.Tenant,l.Project,l.Review,l.Hash,parents.selected);err!=nil{return nil,err}
  return parents.selected.selection.WorkingImageBytes(),nil
 }
 for _,proof:=range parents.parents{if proof.selection.SourceRef()==ref{
  if _,_,err:=adoptedParentProofPacket(ctx,proof.selection.metadata.Source.Bucket,who,l.Tenant,l.Project,proof);err!=nil{return nil,err}
  return proof.selection.WorkingImageBytes(),nil
 }}
 for _,proof:=range p.unchanged{if proof.selection.binding.Source==ref{
  if err:=validateUnchangedStepProof(ctx,p.parents.step,proof);err!=nil{return nil,err}
  return proof.selection.WorkingImageBytes(),nil
 }}
 return nil,v2input.ErrInvalid
}

// CompileNegative repeats mapping/source binding and content qualification. No
// previous execution profile or caller-defined source bytes are accepted. Live
// SQL qualification and task publication remain the admission owner's gates.
func(p PreparedMappedReplacementStep)CompileNegative(ctx context.Context,pin NegativeQualificationPin,locks v2qualification.DeploymentLocks,report,calibration []byte,capacity NegativeResourceEstimate)(NegativeTaskPlan,VerifiedNegativeQualification,error){
 intent,err:=p.Rebind(ctx,nil);if err!=nil{return NegativeTaskPlan{},VerifiedNegativeQualification{},err}
 params,err:=ParseNegativeParams(intent.Params());if err!=nil{return NegativeTaskPlan{},VerifiedNegativeQualification{},err}
 source,err:=p.sourceDocument(ctx,params.Base.Source);if err!=nil{return NegativeTaskPlan{},VerifiedNegativeQualification{},err}
 return intent.CompileNegative(ctx,pin,locks,report,calibration,source,capacity)
}
func(p PreparedMappedReplacementStep)CompilePerspective(ctx context.Context,pin PerspectiveQualificationPin,locks v2qualification.DeploymentLocks,report,calibration []byte,maxPixels int64,capacity PerspectiveResourceEstimate,lineRef *v2input.Ref,lines ...[]byte)(PerspectiveTaskPlan,VerifiedPerspectiveQualification,error){
 intent,err:=p.Rebind(ctx,lineRef);if err!=nil{return PerspectiveTaskPlan{},VerifiedPerspectiveQualification{},err}
 params,err:=ParsePerspectiveParams(intent.Params());if err!=nil{return PerspectiveTaskPlan{},VerifiedPerspectiveQualification{},err}
 source,err:=p.sourceDocument(ctx,params.Base.Source);if err!=nil{return PerspectiveTaskPlan{},VerifiedPerspectiveQualification{},err}
 return intent.CompilePerspective(ctx,pin,locks,report,calibration,source,maxPixels,capacity,lines...)
}

func(p PreparedMappedReplacementStep)CompileMulti(ctx context.Context,pin MultiQualificationPin,locks v2qualification.DeploymentLocks,report,calibration []byte,capacity MultiResourceEstimate)(MultiTaskPlan,VerifiedMultiQualification,error){
 intent,err:=p.Rebind(ctx,nil);if err!=nil{return MultiTaskPlan{},VerifiedMultiQualification{},err}
 var frames []v2input.SessionVersionBinding
 switch intent.Operation(){
 case "optical.panorama":params,err:=ParsePanoramaParams(intent.Params());if err!=nil{return MultiTaskPlan{},VerifiedMultiQualification{},err};frames=params.Frames
 case "optical.focus_stack":params,err:=ParseFocusStackParams(intent.Params());if err!=nil{return MultiTaskPlan{},VerifiedMultiQualification{},err};frames=params.Frames
 default:return MultiTaskPlan{},VerifiedMultiQualification{},v2input.ErrInvalid
 }
 originals:=make([][]byte,0,len(frames))
 for _,frame:=range frames{raw,err:=p.sourceDocument(ctx,frame.Source);if err!=nil{return MultiTaskPlan{},VerifiedMultiQualification{},err};originals=append(originals,raw)}
 return intent.CompileMulti(ctx,pin,locks,report,calibration,originals,capacity)
}
