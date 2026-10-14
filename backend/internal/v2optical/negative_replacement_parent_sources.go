package v2optical

import (
 "context"
 "photoeditor/backend/internal/canonical"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

// PreparedReplacementParentSources binds a ready intent to complete original
// parent array proofs. SQL must still verify replacement membership and retain
// current source/adoption locks before publication.
type PreparedReplacementParentSources struct{
 step PreparedNegativeReplacementStep
 selected VerifiedNegativeSelectedArrays
 parents []VerifiedAdoptedParentArrays
}
func(p PreparedReplacementParentSources)Step()PreparedNegativeReplacementStep{return p.step}
func validAdoptedParentProof(ctx context.Context,p PreparedNegativeReplacementInputs,v VerifiedAdoptedParentArrays)error{
 if err:=ctx.Err();err!=nil{return err}
 s:=v.selection.metadata
 if s.Tenant!=p.tenant||s.Project!=p.project||s.Source.Bucket!=p.bucket||v.who.UserID!=p.who.UserID||v.who.SessionID!=p.who.SessionID||v.selection.who.UserID!=p.who.UserID||v.selection.who.SessionID!=p.who.SessionID{return v2input.ErrInvalid}
 specs,err:=adoptedParentArraySpecs(v.selection);if err!=nil{return err}
 if len(specs)==0||len(specs)!=len(v.specs)||len(specs)!=len(v.objects){return v2input.ErrInvalid}
 for i,spec:=range specs{
  object:=v.objects[i]
  if spec!=v.specs[i]{return v2input.ErrInvalid}
  locator:=negativeSourceLocator{ArtifactID:spec.Ref.ArtifactID,SHA256:object.SHA256,Bucket:p.bucket,Key:object.Key,Version:object.VersionID,Bytes:object.Bytes,Media:object.ContentType}
  raw,err:=canonical.Marshal(locator);if err!=nil{return err}
  checked,err:=decodeNegativeSourceArrayObject(raw,p.bucket,spec)
  if err!=nil||checked!=object{return v2input.ErrInvalid}
 }
 return nil
}

// ReadyParentSources derives completion IDs from physically verified receipts,
// rejects unknown/repeated replacement identities and requires all retained
// parent sources for the next wave. Receipts do not substitute SQL authority.
func(p PreparedNegativeReplacementInputs)ReadyParentSources(ctx context.Context,rebuilt,retained []VerifiedAdoptedParentArrays)([]PreparedReplacementParentSources,error){
 checked,_,_,err:=p.checked(ctx);if err!=nil{return nil,err}
 topology,err:=checked.Topology();if err!=nil{return nil,err}
 if len(rebuilt)>len(topology.Nodes)||len(retained)>4096{return nil,v2input.ErrInvalid}
 nodes:=make(map[uuid.UUID]NegativeReplacementNode,len(topology.Nodes));for _,node:=range topology.Nodes{nodes[node.Task]=node}
 original:=make(map[uuid.UUID]VerifiedOpticalFrozenIntent,len(checked.intents));for _,intent:=range checked.intents{original[intent.locator.Task]=intent}
 proofs:=make(map[uuid.UUID]VerifiedAdoptedParentArrays,len(rebuilt)+len(retained));completed:=make([]uuid.UUID,0,len(rebuilt))
 own:=func(v VerifiedAdoptedParentArrays)VerifiedAdoptedParentArrays{v.specs=append([]NegativeSourceArraySpec(nil),v.specs...);v.objects=append(v.objects[:0:0],v.objects...);return v}
 for _,v:=range rebuilt{
  if err:=validAdoptedParentProof(ctx,checked,v);err!=nil{return nil,err}
  s:=v.selection.metadata;node,known:=nodes[s.Task]
  if !known||s.Run!=node.Run||s.Operation!=original[node.OriginalTask].locator.Operation{return nil,v2input.ErrInvalid}
  if _,duplicate:=proofs[s.Task];duplicate{return nil,v2input.ErrInvalid};proofs[s.Task]=own(v);completed=append(completed,s.Task)
 }
 steps,err:=checked.ReadySteps(ctx,completed);if err!=nil{return nil,err}
 needed:=map[uuid.UUID]bool{};for _,step:=range steps{for _,parent:=range step.node.Parents{if parent.Kind=="retained"{needed[parent.Task]=true}}}
 for _,v:=range retained{
  if err:=validAdoptedParentProof(ctx,checked,v);err!=nil{return nil,err}
  id:=v.selection.metadata.Task
  if !needed[id]{return nil,v2input.ErrInvalid};if _,duplicate:=proofs[id];duplicate{return nil,v2input.ErrInvalid};proofs[id]=own(v)
 }
 for id:=range needed{if _,ok:=proofs[id];!ok{return nil,v2input.ErrInvalid}}
 out:=make([]PreparedReplacementParentSources,0,len(steps))
 for _,step:=range steps{
  entry:=PreparedReplacementParentSources{step:step,selected:checked.physical,parents:[]VerifiedAdoptedParentArrays{}}
  for _,parent:=range step.node.Parents{if parent.Kind!="selected_base"{proof,ok:=proofs[parent.Task];if !ok{return nil,v2input.ErrInvalid};entry.parents=append(entry.parents,proof)}}
  out=append(out,entry)
 }
 return out,ctx.Err()
}
