package v2optical

import (
 "context"
 "sort"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
)

// LockCompleteMappedSources validates the entire wave including unchanged
// source proofs before SQL, then retains all source authority in one caller
// transaction. No IO/commit/publication; roll back every refusal. Replacement
// membership and current qualification/task admission remain required.
func(p PreparedNegativeReplacementInputs)LockCompleteMappedSources(ctx context.Context,tx pgx.Tx,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources,unchanged []VerifiedNegativeUnchangedArrays)([]PreparedMappedReplacementStep,error){
 if err:=ctx.Err();err!=nil{return nil,err};if tx==nil||len(unchanged)>4096*30{return nil,v2input.ErrInvalid}
 out,err:=p.PrepareCompleteMappedSources(ctx,rebuilt,retained,mappings,unchanged);if err!=nil{return nil,err}
 locks:=make([]VerifiedNegativeUnchangedArrays,0,len(unchanged))
 for _,step:=range out{locks=append(locks,step.unchanged...)}
 if _,err=p.LockMappedReadySources(ctx,tx,rebuilt,retained,mappings);err!=nil{return nil,err}
 sort.Slice(locks,func(i,j int)bool{
  a,b:=locks[i].selection,locks[j].selection
  if a.step.node.OriginalTask!=b.step.node.OriginalTask{return a.step.node.OriginalTask.String()<b.step.node.OriginalTask.String()}
  return a.binding.Source.ArtifactID.String()<b.binding.Source.ArtifactID.String()
 })
 for _,proof:=range locks{if err=p.LockUnchangedSource(ctx,tx,proof);err!=nil{return nil,err}}
 return out,ctx.Err()
}

// PrepareCompleteMappedSources performs pure complete-wave preparation before
// the admission transaction, so source recompilation/content qualification need
// not hold SQL locks. The eventual transaction must call LockCompleteMappedSources
// again and bind the compiled task Params/source/qualification to these inputs.
// This method emits no queue receipt and performs no object or database IO.
func(p PreparedNegativeReplacementInputs)PrepareCompleteMappedSources(ctx context.Context,rebuilt,retained []VerifiedAdoptedParentArrays,mappings []VerifiedNegativeAffectedSources,unchanged []VerifiedNegativeUnchangedArrays)([]PreparedMappedReplacementStep,error){
 if err:=ctx.Err();err!=nil{return nil,err};if len(unchanged)>4096*30{return nil,v2input.ErrInvalid}
 ready,err:=p.prepareMappedReadySources(ctx,rebuilt,retained,mappings);if err!=nil{return nil,err}
 known:=make(map[uuid.UUID]bool,len(ready));for _,step:=range ready{known[step.Node().Task]=true}
 groups:=make(map[uuid.UUID][]VerifiedNegativeUnchangedArrays)
 for _,proof:=range unchanged{
  id:=proof.selection.step.node.Task;if !known[id]{return nil,v2input.ErrInvalid}
  groups[id]=append(groups[id],proof);if len(groups[id])>30{return nil,v2input.ErrInvalid}
 }
 out:=make([]PreparedMappedReplacementStep,0,len(ready))
 for _,step:=range ready{
  complete,err:=step.WithUnchangedSources(ctx,groups[step.Node().Task]);if err!=nil{return nil,err}
  out=append(out,complete)
 }
 return out,ctx.Err()
}
