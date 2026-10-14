package v2optical

import (
 "bytes"
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// CompileNegativeReplacementFrontier selects the next independent compilation
// wave. A downstream operation needs its rebuilt parents' adopted physical
// outputs before its own Params/source qualification can be compiled. Completed
// IDs are advisory here: admission must independently verify persisted adoption
// and physical source proof. This function never acknowledges or queues a task.
func CompileNegativeReplacementFrontier(ctx context.Context,topology NegativeReplacementTopology,plan NegativeInvalidationPlan,inventory []NegativeDependencyTask,completed []uuid.UUID)([]NegativeReplacementNode,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if topology.Route!="base"||len(completed)>4096{return nil,v2input.ErrInvalid}
 expected,err:=CompileNegativeReplacementTopology(ctx,topology.Tenant,topology.Project,topology.Review,topology.DecisionHash,plan,inventory,topology.SelectedBase,4096,65536)
 if err!=nil{return nil,err}
 a,err:=canonical.Marshal(expected);if err!=nil{return nil,err};b,err:=canonical.Marshal(topology)
 if err!=nil||!bytes.Equal(a,b){return nil,v2input.ErrInvalid}
 known:=make(map[uuid.UUID]NegativeReplacementNode,len(expected.Nodes))
 for _,node:=range expected.Nodes{known[node.Task]=node}
 done:=make(map[uuid.UUID]bool,len(completed))
 for _,id:=range completed{
  if _,ok:=known[id];!ok||done[id]{return nil,v2input.ErrInvalid};done[id]=true
 }
 // Recovery cannot claim a descendant is complete while forgetting its rebuilt
 // prerequisites. Selected base and retained parents are not replacement IDs.
 for id:=range done{for _,parent:=range known[id].Parents{
  if parent.Kind=="replacement"&&!done[parent.Task]{return nil,v2input.ErrInvalid}
 }}
 out:=make([]NegativeReplacementNode,0)
 for _,node:=range expected.Nodes{
  if err:=ctx.Err();err!=nil{return nil,err};if done[node.Task]{continue}
  ready:=true;for _,parent:=range node.Parents{if parent.Kind=="replacement"&&!done[parent.Task]{ready=false;break}}
  if ready{node.Parents=append([]NegativeReplacementParent(nil),node.Parents...);out=append(out,node)}
 }
 return out,ctx.Err()
}
