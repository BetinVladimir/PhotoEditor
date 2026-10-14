package v2optical

import (
 "bytes"
 "context"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

func validateUnchangedStepProof(ctx context.Context,step PreparedNegativeReplacementStep,proof VerifiedNegativeUnchangedArrays)error{
 if err:=ctx.Err();err!=nil{return err}
 source:=proof.selection;who:=step.intent.who
 a,err:=canonical.Marshal(step.node);if err!=nil{return err};b,err:=canonical.Marshal(source.step.node)
 if err!=nil||!bytes.Equal(a,b)||step.intent.locator!=source.step.intent.locator||step.intent.raw!=source.step.intent.raw||proof.who.UserID!=who.UserID||proof.who.SessionID!=who.SessionID||source.step.intent.who.UserID!=who.UserID||source.step.intent.who.SessionID!=who.SessionID{return v2input.ErrInvalid}
 specs,err:=unchangedSourceArraySpecs(source);if err!=nil{return err}
 if len(specs)==0||len(specs)!=len(proof.specs)||len(specs)!=len(proof.objects){return v2input.ErrInvalid}
 for i,spec:=range specs{
  if spec!=proof.specs[i]{return v2input.ErrInvalid};object:=proof.objects[i]
  locator:=negativeSourceLocator{ArtifactID:spec.Ref.ArtifactID,SHA256:object.SHA256,Bucket:source.locator.Bucket,Key:object.Key,Version:object.VersionID,Bytes:object.Bytes,Media:object.ContentType}
  raw,err:=canonical.Marshal(locator);if err!=nil{return err};checked,err:=decodeNegativeSourceArrayObject(raw,source.locator.Bucket,spec)
  if err!=nil||checked!=object{return v2input.ErrInvalid}
 }
 return nil
}

// WithUnchangedSources owns a complete set of unchanged physical inputs absent
// from parent proofs. Refuses changed-source, duplicate, extra or missing proofs.
// Physical preparation is not SQL admission: LockUnchangedSource remains required.
func(p PreparedMappedReplacementStep)WithUnchangedSources(ctx context.Context,proofs []VerifiedNegativeUnchangedArrays)(PreparedMappedReplacementStep,error){
 var zero PreparedMappedReplacementStep
 if err:=ctx.Err();err!=nil{return zero,err};if len(proofs)>30{return zero,v2input.ErrInvalid}
 required,err:=p.requiredUnchangedBindings(ctx);if err!=nil{return zero,err}
 p.unchanged=nil // Replace the full attached set; do not infer completeness from old attachments.
 needed:=map[v2input.SessionVersionBinding]bool{}
 for _,binding:=range required{needed[binding]=true}
 if len(proofs)!=len(needed){return zero,v2input.ErrInvalid}
 owned:=make([]VerifiedNegativeUnchangedArrays,0,len(proofs))
 for _,proof:=range proofs{
  binding:=proof.selection.binding
  if !needed[binding]{return zero,v2input.ErrInvalid}
  if err:=validateUnchangedStepProof(ctx,p.parents.step,proof);err!=nil{return zero,err}
  delete(needed,binding)
  proof.selection.step.node.Parents=append([]NegativeReplacementParent(nil),proof.selection.step.node.Parents...)
  proof.specs=append([]NegativeSourceArraySpec(nil),proof.specs...);proof.objects=append([]objectstore.Object(nil),proof.objects...)
  owned=append(owned,proof)
 }
 if len(needed)!=0{return zero,v2input.ErrInvalid};p.unchanged=owned;return p,ctx.Err()
}

// Preserve frozen frame order and deduplicate identical occurrences. Discovery
// and attachment use this same rule so a source cannot be silently omitted.
func(p PreparedMappedReplacementStep)requiredUnchangedBindings(ctx context.Context)([]v2input.SessionVersionBinding,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 originals,err:=opticalFrozenBindings(p.parents.step.Operation(),[]byte(p.parents.step.intent.raw));if err!=nil{return nil,err}
 changed:=map[v2input.Ref]bool{};for _,entry:=range p.mapping.sources{changed[entry.Before.Source]=true}
 p.unchanged=nil
 seen:=map[v2input.SessionVersionBinding]bool{}
 out:=make([]v2input.SessionVersionBinding,0)
 for _,binding:=range originals{
  if changed[binding.Source]||seen[binding]{continue}
  if _,err:=p.sourceDocument(ctx,binding.Source);err==nil{continue}
  if err:=ctx.Err();err!=nil{return nil,err}
  seen[binding]=true;out=append(out,binding)
 }
 if len(out)>30{return nil,v2input.ErrInvalid}
 return out,nil
}
