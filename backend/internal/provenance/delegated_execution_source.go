package provenance

import (
 "bytes"
 "context"
 "encoding/json"
 "sort"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// DelegatedTaskReader must be scoped to the frozen parent delegation. The
// caller cannot substitute a task ID that SQL did not prove as an ancestor.
type DelegatedTaskReader interface {
 ReadDelegatedTask(context.Context,uuid.UUID)(CanonicalSigningTask,error)
}

// DelegatedTaskClaims resolves operation-specific immutable documents and
// content parents without accepting a fabricated editor session.
type DelegatedTaskClaims interface {
 ResolveDelegatedTask(context.Context,ReadySigningExport,
  CanonicalSigningTask)(ResolvedSigningTask,error)
}

type BoundDelegatedSigningExecutionSource struct {
 Tasks DelegatedTaskReader
 Claims DelegatedTaskClaims
}

func(s BoundDelegatedSigningExecutionSource)ReadDelegatedSigningExecution(
 ctx context.Context,ready ReadySigningExport)(SigningExecutionGraph,error){
 var zero SigningExecutionGraph
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Tasks==nil||s.Claims==nil||ready.Task==uuid.Nil||ready.Run==uuid.Nil||
  ready.Tenant==uuid.Nil||ready.Project==uuid.Nil||ready.ExportID==uuid.Nil||
  !ref(ready.Ref){return zero,ErrInvalid}
 graph:=SigningExecutionGraph{FinalTask:ready.Task,Export:ready.Ref}
 visited:=map[uuid.UUID]bool{}
 sources:=map[uuid.UUID]v2input.Ref{}
 queue:=[]uuid.UUID{ready.Task}
 totalItems,totalBytes:=0,0
 for len(queue)>0{
  if err:=ctx.Err();err!=nil{return zero,err}
  id:=queue[len(queue)-1]
  queue=queue[:len(queue)-1]
  if visited[id]{continue}
  visited[id]=true
  if len(visited)>5000{return zero,ErrInvalid}
  evidence,err:=s.Tasks.ReadDelegatedTask(ctx,id)
  if err!=nil{return zero,err}
  if evidence.Task!=id||normalizeCanonicalSigningTask(&evidence)!=nil{return zero,ErrInvalid}
  if id==ready.Task&&evidence.Run!=ready.Run{return zero,ErrInvalid}
  proof,err:=canonical.Marshal(evidence)
  if err!=nil{return zero,ErrInvalid}
  totalBytes+=len(proof)
  if totalBytes>64<<20{return zero,ErrInvalid}
  var owned CanonicalSigningTask
  if json.Unmarshal(proof,&owned)!=nil{return zero,ErrInvalid}
  resolved,err:=s.Claims.ResolveDelegatedTask(ctx,ready,owned)
  if err!=nil{return zero,err}
  raw,err:=canonical.Marshal(resolved)
  if err!=nil||len(raw)>16<<20{return zero,ErrInvalid}
  var bound ResolvedSigningTask
  if json.Unmarshal(raw,&bound)!=nil{return zero,ErrInvalid}
  node:=bound.Node
  if node.Task!=evidence.Task||node.Run!=evidence.Run||
   node.CanonicalResult!=evidence.Result||node.Attempt!=evidence.Attempt||
   node.Fence!=evidence.Fence||node.Action.Operation!=evidence.Operation||
   node.Action.Version!=evidence.Version||
   node.Action.ParamsSHA!=evidence.Params.SHA256{return zero,ErrInvalid}
  outputs,err:=canonicalTaskOutputRefs(evidence.Outputs)
  if err!=nil||!sameSigningRefs(node.Outputs,outputs){return zero,ErrInvalid}
  if _,err:=signingNodeContentOutputs(node);err!=nil{return zero,err}
  current,err:=s.Tasks.ReadDelegatedTask(ctx,id)
  if err!=nil{return zero,err}
  if normalizeCanonicalSigningTask(&current)!=nil{return zero,ErrInvalid}
  currentProof,err:=canonical.Marshal(current)
  if err!=nil||!bytes.Equal(proof,currentProof){return zero,ErrInvalid}
  totalItems+=len(node.Parents)+len(node.Inputs)+len(node.Outputs)+
   len(node.Children)+len(bound.Sources)
  if totalItems>100000{return zero,ErrInvalid}
  for _,source:=range bound.Sources{
   if !ref(source){return zero,ErrInvalid}
   if old,ok:=sources[source.ArtifactID];ok&&old!=source{return zero,ErrInvalid}
   sources[source.ArtifactID]=source
  }
  if len(sources)>5000{return zero,ErrInvalid}
  graph.Nodes=append(graph.Nodes,node)
  queue=append(queue,node.Parents...)
 }
 for _,source:=range sources{graph.Sources=append(graph.Sources,source)}
 sort.Slice(graph.Sources,func(i,j int)bool{
  return graph.Sources[i].ArtifactID.String()<graph.Sources[j].ArtifactID.String()})
 sort.Slice(graph.Nodes,func(i,j int)bool{
  return graph.Nodes[i].Task.String()<graph.Nodes[j].Task.String()})
 if _,err:=CompileSigningLineage(ctx,graph);err!=nil{return zero,err}
 return graph,nil
}
