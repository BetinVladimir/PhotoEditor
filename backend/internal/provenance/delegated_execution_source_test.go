package provenance

import (
 "context"
 "encoding/json"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type delegatedTaskFunc func(context.Context,uuid.UUID)(CanonicalSigningTask,error)
func(f delegatedTaskFunc)ReadDelegatedTask(ctx context.Context,id uuid.UUID)(CanonicalSigningTask,error){return f(ctx,id)}
type delegatedClaimsFunc func(context.Context,ReadySigningExport,CanonicalSigningTask)(ResolvedSigningTask,error)
func(f delegatedClaimsFunc)ResolveDelegatedTask(ctx context.Context,ready ReadySigningExport,
 task CanonicalSigningTask)(ResolvedSigningTask,error){return f(ctx,ready,task)}

func TestDelegatedExecutionRejectsChangedCanonicalTask(t *testing.T){
 for _,mode:=range []string{"valid","changed","claim","missing"}{
  t.Run(mode,func(t *testing.T){
   fixture:=signingGraphFixture()
   nodes:=map[uuid.UUID]SigningExecutionNode{}
   for _,node:=range fixture.Nodes{nodes[node.Task]=node}
   final:=nodes[fixture.FinalTask]
   ready:=ReadySigningExport{Tenant:uuid.New(),Project:uuid.New(),
    ExportID:uuid.New(),Task:fixture.FinalTask,Run:final.Run,Ref:fixture.Export}
   reads:=map[uuid.UUID]int{}
   tasks:=delegatedTaskFunc(func(_ context.Context,id uuid.UUID)(CanonicalSigningTask,error){
    node,ok:=nodes[id]
    if !ok||mode=="missing"{return CanonicalSigningTask{},ErrInvalid}
    reads[id]++
    inputs,_:=canonical.Marshal(node.Inputs)
    outputs,_:=canonical.Marshal(node.Outputs)
    fence:=node.Fence
    if mode=="changed"&&reads[id]>1{fence++}
    return CanonicalSigningTask{Task:id,Run:node.Run,Result:node.CanonicalResult,
     Attempt:node.Attempt,Fence:fence,Operation:node.Action.Operation,
     Version:node.Action.Version,InputDigest:strings.Repeat("a",64),
     ResultDigest:strings.Repeat("b",64),
     Params:v2input.Ref{ArtifactID:uuid.NewSHA1(id,[]byte("params")),
      SHA256:node.Action.ParamsSHA},Inputs:inputs,Outputs:outputs,
     Metrics:json.RawMessage(`{}`)},nil
   })
   claims:=delegatedClaimsFunc(func(_ context.Context,_ ReadySigningExport,
    task CanonicalSigningTask)(ResolvedSigningTask,error){
    node:=nodes[task.Task]
    if mode=="claim"{node.Action.ParamsSHA=strings.Repeat("f",64)}
    result:=ResolvedSigningTask{Node:node}
    if len(node.Parents)==0{result.Sources=fixture.Sources}
    return result,nil
   })
   graph,err:=(BoundDelegatedSigningExecutionSource{Tasks:tasks,Claims:claims}).
    ReadDelegatedSigningExecution(context.Background(),ready)
   if mode=="valid"{
    if err!=nil||len(graph.Nodes)!=2||len(graph.Sources)!=1{
     t.Fatal("delegated graph lost",err)
    }
   }else if err==nil{t.Fatal("invalid delegated graph accepted",mode)}
  })
 }
}
