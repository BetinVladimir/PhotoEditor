package v2optical

import (
 "bytes"
 "context"
 "sort"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type PreparedMappedReplacementStep struct{parents PreparedReplacementParentSources;mapping VerifiedNegativeAffectedSources;unchanged []VerifiedNegativeUnchangedArrays}
func(p PreparedMappedReplacementStep)Node()NegativeReplacementNode{return p.parents.Step().Node()}
func(p PreparedMappedReplacementStep)Rebind(ctx context.Context,lines *v2input.Ref)(ReboundOpticalIntent,error){return p.parents.RebindMapped(ctx,p.mapping,lines)}

// LockMappedReadySources requires exactly one scoped map for every ready node,
// validates all packets before SQL, then retains physical-parent and mapping
// authority in the caller's transaction. Returned steps do not grant queueing:
// replacement membership, current qualification and admission remain required.
func(p PreparedNegativeReplacementInputs)LockMappedReadySources(ctx context.Context,tx pgx.Tx,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources)([]PreparedMappedReplacementStep,error){
 if err:=ctx.Err();err!=nil{return nil,err};if tx==nil{return nil,v2input.ErrInvalid}
 checked,_,_,err:=p.checked(ctx);if err!=nil{return nil,err}
 out,err:=checked.prepareMappedReadySources(ctx,rebuilt,retained,mappings);if err!=nil{return nil,err}
 if _,err=checked.LockReadyParentSources(ctx,tx,rebuilt,retained);err!=nil{return nil,err}
 // Preserve frontier order in returned work, but lock original map consumers in
 // UUID order independently of dependency ordering.
 ordered:=append([]PreparedMappedReplacementStep(nil),out...)
 sort.Slice(ordered,func(i,j int)bool{return ordered[i].mapping.step.node.OriginalTask.String()<ordered[j].mapping.step.node.OriginalTask.String()})
 for _,entry:=range ordered{if err=checked.LockAffectedSources(ctx,tx,entry.mapping);err!=nil{return nil,err}}
 return out,ctx.Err()
}

func(p PreparedNegativeReplacementInputs)prepareMappedReadySources(ctx context.Context,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources)([]PreparedMappedReplacementStep,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 checked,_,_,err:=p.checked(ctx);if err!=nil{return nil,err}
 ready,err:=checked.ReadyParentSources(ctx,rebuilt,retained);if err!=nil{return nil,err}
 if len(mappings)!=len(ready){return nil,v2input.ErrInvalid}
 byTask:=make(map[uuid.UUID]VerifiedNegativeAffectedSources,len(mappings))
 for _,mapping:=range mappings{
  id:=mapping.step.node.Task;if _,duplicate:=byTask[id];duplicate{return nil,v2input.ErrInvalid}
  byTask[id]=mapping
 }
 out:=make([]PreparedMappedReplacementStep,0,len(ready))
 for _,parent:=range ready{
  mapping,ok:=byTask[parent.step.node.Task];if !ok{return nil,v2input.ErrInvalid}
  a,err:=canonical.Marshal(parent.step.node);if err!=nil{return nil,err};b,err:=canonical.Marshal(mapping.step.node)
  if err!=nil||!bytes.Equal(a,b)||parent.step.intent.locator!=mapping.step.intent.locator||parent.step.intent.raw!=mapping.step.intent.raw||parent.step.intent.who.UserID!=mapping.step.intent.who.UserID||parent.step.intent.who.SessionID!=mapping.step.intent.who.SessionID{return nil,v2input.ErrInvalid}
  matched,err:=matchNegativeAffectedSources(ctx,parent.step.Operation(),[]byte(parent.step.intent.raw),parent.step.node.Parents,[]byte(mapping.raw));if err!=nil{return nil,err}
  a,err=canonical.Marshal(matched);if err!=nil{return nil,err};b,err=canonical.Marshal(mapping.sources)
  if err!=nil||!bytes.Equal(a,b){return nil,v2input.ErrInvalid}
  mapping.step.node.Parents=append([]NegativeReplacementParent(nil),mapping.step.node.Parents...)
  mapping.sources=append(make([]matchedNegativeAffectedSource,0,len(mapping.sources)),mapping.sources...)
  out=append(out,PreparedMappedReplacementStep{parents:parent,mapping:mapping})
 }
 return out,ctx.Err()
}
