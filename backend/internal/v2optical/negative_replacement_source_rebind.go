package v2optical

import (
 "context"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// Rebind allows new sources only from the selected base or exact rebuilt parent
// receipts attached to this ready step. Retained parents are never substitute
// outputs. Fresh line qualification and SQL admission remain separate gates.
func(p PreparedReplacementParentSources)Rebind(ctx context.Context,bindings []OpticalReplacementBinding,lines *v2input.Ref)(ReboundOpticalIntent,error){
 var zero ReboundOpticalIntent
 if err:=ctx.Err();err!=nil{return zero,err}
 if len(bindings)<1||len(bindings)>30{return zero,v2input.ErrInvalid}
 allowed:=map[v2input.Ref]string{}
 add:=func(ref v2input.Ref,raw []byte)error{
  image,err:=review.ParseWorkingImage(raw);if err!=nil{return v2input.ErrInvalid}
  canvas,err:=canonical.Hash(image.Canvas);if err!=nil{return err}
  if existing,ok:=allowed[ref];ok&&existing!=canvas{return v2input.ErrInvalid};allowed[ref]=canvas;return nil
 }
 l:=p.step.intent.locator;who:=p.step.intent.who
 for _,parent:=range p.step.node.Parents{
  switch parent.Kind{
  case "selected_base":
   if _,_,err:=selectedNegativeProofPacket(selectedSourceBucket(p.selected.selection),who,l.Tenant,l.Project,l.Review,l.Hash,p.selected);err!=nil{return zero,err}
   ref:=p.selected.selection.SelectedBase();if ref==nil{return zero,v2input.ErrInvalid}
   if err:=add(*ref,p.selected.selection.WorkingImageBytes());err!=nil{return zero,err}
  case "replacement":
   found:=false
   for _,proof:=range p.parents{if proof.selection.metadata.Task==parent.Task{
    scope:=PreparedNegativeReplacementInputs{bucket:proof.selection.metadata.Source.Bucket,who:who,tenant:l.Tenant,project:l.Project}
    if err:=validAdoptedParentProof(ctx,scope,proof);err!=nil{return zero,err}
    if err:=add(proof.selection.SourceRef(),proof.selection.WorkingImageBytes());err!=nil{return zero,err};found=true
   }}
   if !found{return zero,v2input.ErrInvalid}
  case "retained":
  default:return zero,v2input.ErrInvalid
  }
 }
 used:=map[v2input.Ref]bool{}
 for _,binding:=range bindings{
  canvas,ok:=allowed[binding.After.Source]
  if !ok||binding.After.CanvasHash!=canvas||used[binding.After.Source]{return zero,v2input.ErrInvalid}
  used[binding.After.Source]=true
 }
 return p.step.Rebind(ctx,bindings,lines)
}

func selectedSourceBucket(v VerifiedNegativeSelection)string{if v.source.Source==nil{return ""};return v.source.Source.Bucket}
