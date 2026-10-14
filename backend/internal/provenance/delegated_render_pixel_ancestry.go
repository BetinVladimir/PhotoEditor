package provenance

import (
 "context"
 "sort"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// ResolveDelegatedRenderPixelAncestry binds every typed image/result input to
// its current direct or adopted delegated producer, twice across external IO.
func ResolveDelegatedRenderPixelAncestry(ctx context.Context,
 producers DelegatedImageProducer,task CanonicalSigningTask,
 inventory SigningRenderCausalInventory)(signingRenderPixelAncestry,error){
 var zero signingRenderPixelAncestry
 if err:=ctx.Err();err!=nil{return zero,err}
 taskHash,err:=canonical.Hash(task)
 if err!=nil||producers==nil||task.Task==uuid.Nil||
  task.Operation!="render.compose"||task.Version!="1"||
  taskHash!=inventory.taskDigest||len(inventory.Images)<1{
  return zero,ErrInvalid
 }
 inputs:=append([]v2input.Ref(nil),inventory.Images...)
 inputs=append(inputs,inventory.Results...)
 if len(inputs)>4096{return zero,ErrInvalid}
 seen:=map[uuid.UUID]bool{}
 parents:=map[uuid.UUID]bool{}
 bound:=make([]uuid.UUID,len(inputs))
 for _,input:=range inputs{
  if !ref(input)||seen[input.ArtifactID]{return zero,ErrInvalid}
  seen[input.ArtifactID]=true
 }
 for i,input:=range inputs{
  if err:=ctx.Err();err!=nil{return zero,err}
  producer,err:=producers.ResolveDelegatedImageProducer(ctx,input)
  if err!=nil{return zero,err}
  if producer==uuid.Nil||producer==task.Task{return zero,ErrInvalid}
  bound[i]=producer
  parents[producer]=true
 }
 for i,input:=range inputs{
  if err:=ctx.Err();err!=nil{return zero,err}
  current,err:=producers.ResolveDelegatedImageProducer(ctx,input)
  if err!=nil{return zero,err}
  if current!=bound[i]{return zero,ErrInvalid}
 }
 if err:=ctx.Err();err!=nil{return zero,err}
 ordered:=make([]uuid.UUID,0,len(parents))
 for id:=range parents{ordered=append(ordered,id)}
 sort.Slice(ordered,func(i,j int)bool{
  return ordered[i].String()<ordered[j].String()})
 return signingRenderPixelAncestry{inputs:inputs,parents:ordered,
  producers:bound},nil
}
