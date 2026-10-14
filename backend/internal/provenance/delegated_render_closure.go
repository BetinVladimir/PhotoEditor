package provenance

import (
 "bytes"
 "context"
 "encoding/json"

 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

type DelegatedRenderClosureResolver interface {
 ResolveDelegatedTaskInputEvidence(context.Context,CanonicalSigningTask,
  []v2input.Entry,v2input.SessionVersionBinding)(v2draft.TaskInputEvidence,error)
}

type BoundDelegatedRenderClosureSource struct {
 Documents DelegatedTaskDocuments
 Closure DelegatedRenderClosureResolver
}

func(s BoundDelegatedRenderClosureSource)ReadDelegatedRenderClosure(ctx context.Context,
 task CanonicalSigningTask)(SigningRenderClosureEvidence,error){
 var zero SigningRenderClosureEvidence
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Documents==nil||s.Closure==nil{return zero,ErrInvalid}
 owned,err:=ownSigningProducerTask(task)
 if err!=nil{return zero,err}
 recipe,err:=ReadDelegatedRenderRecipe(ctx,s.Documents,owned)
 if err!=nil{return zero,err}
 raw,err:=canonical.Marshal(recipe)
 if err!=nil{return zero,err}
 roots:=[]v2input.Entry{{Ref:recipe.Scalars.Source.Source,Contract:"WorkingImage"},
  {Ref:owned.Params,Contract:"FrozenRenderRecipe"}}
 read:=func()(v2draft.TaskInputEvidence,[]byte,error){
  isolated,err:=ownSigningProducerTask(owned)
  if err!=nil{return v2draft.TaskInputEvidence{},nil,err}
  evidence,err:=s.Closure.ResolveDelegatedTaskInputEvidence(ctx,isolated,
   append([]v2input.Entry(nil),roots...),recipe.Scalars.Source)
  if err!=nil{return v2draft.TaskInputEvidence{},nil,err}
  if err:=verifySigningRenderInputDigest(owned,evidence.Entries);err!=nil{
   return v2draft.TaskInputEvidence{},nil,err
  }
  found:=false
  for _,document:=range evidence.Documents{
   if document.Ref==owned.Params&&document.Contract=="FrozenRenderRecipe"{
    if found||document.JCS!=string(raw){return v2draft.TaskInputEvidence{},nil,ErrInvalid}
    found=true
   }
  }
  if !found||evidence.Entries==nil||evidence.Occurrences==nil{
   return v2draft.TaskInputEvidence{},nil,ErrInvalid
  }
  frozen,err:=canonical.Marshal(evidence)
  if err!=nil||len(frozen)>64<<20{return v2draft.TaskInputEvidence{},nil,ErrInvalid}
  var result v2draft.TaskInputEvidence
  if json.Unmarshal(frozen,&result)!=nil{return v2draft.TaskInputEvidence{},nil,ErrInvalid}
  return result,frozen,nil
 }
 inventory,proof,err:=read()
 if err!=nil{return zero,err}
 if err:=VerifyDelegatedRenderTransport(ctx,s.Documents,owned,inventory.Entries);err!=nil{
  return zero,err
 }
 _,current,err:=read()
 if err!=nil{return zero,err}
 if !bytes.Equal(proof,current){return zero,ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,err}
 digest,err:=canonical.Hash(owned)
 if err!=nil{return zero,err}
 return SigningRenderClosureEvidence{taskDigest:digest,inventory:inventory},nil
}

var _ DelegatedRenderClosureReader=BoundDelegatedRenderClosureSource{}
