package v2optical

import (
 "bytes"
 "context"
 "sort"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

// Topology is a deterministic recompile instruction, not an executable task.
// A dedicated operation compiler must restore the frozen original intent, bind
// the selected/rebuilt physical source and repeat qualification/admission.
type NegativeReplacementParent struct {
 Original uuid.UUID `json:"original_task_id"`
 Task uuid.UUID `json:"task_id"`
 Kind string `json:"kind"` // selected_base, replacement, retained
}
type NegativeReplacementNode struct {
 OriginalTask uuid.UUID `json:"original_task_id"`
 OriginalRun uuid.UUID `json:"original_run_id"`
 Task uuid.UUID `json:"task_id"`
 Run uuid.UUID `json:"run_id"`
 Parents []NegativeReplacementParent `json:"parents"`
}
type NegativeReplacementTopology struct {
 SchemaVersion int `json:"schema_version"`
 Tenant uuid.UUID `json:"tenant_id"`
 Project uuid.UUID `json:"project_id"`
 Review uuid.UUID `json:"review_id"`
 DecisionHash string `json:"decision_hash"`
 Route string `json:"route"`
 SelectedBase *v2input.Ref `json:"selected_base"`
 Nodes []NegativeReplacementNode `json:"nodes"`
 OmittedTasks []uuid.UUID `json:"omitted_tasks"`
}

// CompileNegativeReplacementTopology independently recomputes the affected DAG
// and refuses altered invalidation plans. UUIDv5 binds every new identity to the
// tenant, project, review, decision hash and original run/task. Each original
// task has a distinct replacement run so sealed admission never freezes unknown
// future output SHA values. No original
// operation, Params, task result or lease is mutated/copied into a runnable task.
func CompileNegativeReplacementTopology(ctx context.Context,tenant,project,review uuid.UUID,decisionHash string,plan NegativeInvalidationPlan,inventory []NegativeDependencyTask,selectedBase *v2input.Ref,maxTasks,maxEdges int)(NegativeReplacementTopology,error) {
 var zero NegativeReplacementTopology
 if err:=ctx.Err();err!=nil {return zero,err}
 if tenant==uuid.Nil||project==uuid.Nil||review==uuid.Nil {return zero,v2input.ErrInvalid}
 if _,err:=domain.ParseDigest(decisionHash);err!=nil {return zero,v2input.ErrInvalid}
 expected,err:=PlanNegativeInvalidation(ctx,plan.OriginTask,plan.OriginRun,plan.Route,inventory,maxTasks,maxEdges);if err!=nil {return zero,err}
 a,err:=canonical.Marshal(expected);if err!=nil {return zero,err};b,err:=canonical.Marshal(plan)
 if err!=nil||!bytes.Equal(a,b) {return zero,v2input.ErrInvalid}
 out:=NegativeReplacementTopology{SchemaVersion:2,Tenant:tenant,Project:project,Review:review,DecisionHash:decisionHash,Route:plan.Route,Nodes:[]NegativeReplacementNode{},OmittedTasks:[]uuid.UUID{}}
 if plan.Route=="skip" {
  if selectedBase!=nil {return zero,v2input.ErrInvalid}
  out.OmittedTasks=append(out.OmittedTasks,plan.AffectedTasks...)
  return out,ctx.Err()
 }
 if selectedBase==nil||selectedBase.ArtifactID==uuid.Nil {return zero,v2input.ErrInvalid}
 if _,err:=domain.ParseDigest(selectedBase.SHA256);err!=nil {return zero,v2input.ErrInvalid}
 owned:=*selectedBase;out.SelectedBase=&owned
 namespace:=uuid.NewSHA1(tenant,[]byte("V29.NegativeReplacement/v2:"+project.String()+":"+review.String()+":"+decisionHash))
 tasks:=make(map[uuid.UUID]NegativeDependencyTask,len(inventory));affected:=make(map[uuid.UUID]bool,len(plan.AffectedTasks))
 for _,d:=range inventory {d.Parents=append([]uuid.UUID(nil),d.Parents...);tasks[d.TaskID]=d}
 for _,id:=range plan.AffectedTasks {affected[id]=true}
 indegree:=make(map[uuid.UUID]int,len(affected));children:=make(map[uuid.UUID][]uuid.UUID)
 for id:=range affected {for _,parent:=range tasks[id].Parents {if affected[parent] {indegree[id]++;children[parent]=append(children[parent],id)}}}
 ready:=[]uuid.UUID{};for id:=range affected {if indegree[id]==0 {ready=append(ready,id)}}
 less:=func(ids []uuid.UUID){sort.Slice(ids,func(i,j int)bool{return bytes.Compare(ids[i][:],ids[j][:])<0})}
 for len(ready)>0 {
  if err:=ctx.Err();err!=nil {return zero,err};less(ready);id:=ready[0];ready=ready[1:];old:=tasks[id]
  node:=NegativeReplacementNode{OriginalTask:id,OriginalRun:old.RunID,Task:uuid.NewSHA1(namespace,[]byte("task:"+id.String())),Run:uuid.NewSHA1(namespace,[]byte("run:"+old.RunID.String()+":"+id.String())),Parents:[]NegativeReplacementParent{}}
  less(old.Parents)
  for _,parent:=range old.Parents {
   binding:=NegativeReplacementParent{Original:parent,Task:parent,Kind:"retained"}
   if parent==plan.OriginTask {binding.Task=uuid.Nil;binding.Kind="selected_base"} else if affected[parent] {binding.Task=uuid.NewSHA1(namespace,[]byte("task:"+parent.String()));binding.Kind="replacement"}
   node.Parents=append(node.Parents,binding)
  }
  out.Nodes=append(out.Nodes,node)
  for _,child:=range children[id] {indegree[child]--;if indegree[child]==0 {ready=append(ready,child)}}
 }
 if len(out.Nodes)!=len(affected) {return zero,v2input.ErrInvalid}
 return out,ctx.Err()
}
