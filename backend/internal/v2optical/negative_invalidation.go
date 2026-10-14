package v2optical

import (
 "context"
 "sort"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

// Inventory must be loaded completely under current tenant/project authority.
// This pure planner does not establish completeness, cancel work or rewrite an
// immutable result. A later transaction must revalidate and apply its output.
type NegativeDependencyTask struct {
 TaskID uuid.UUID `json:"task_id"`
 RunID uuid.UUID `json:"run_id"`
 // Exact canonical outputs actually consumed, resolved by the scoped catalogue.
 Parents []uuid.UUID `json:"parents"`
}
type NegativeInvalidationPlan struct {
 SchemaVersion int `json:"schema_version"`
 OriginTask uuid.UUID `json:"origin_task_id"`
 OriginRun uuid.UUID `json:"origin_run_id"`
 Route string `json:"route"`
 AffectedTasks []uuid.UUID `json:"affected_tasks"`
 ConsumerRuns []uuid.UUID `json:"consumer_runs"`
 RecompileOrigin bool `json:"recompile_origin"`
}

// PlanNegativeInvalidation follows transitive producer dependencies. Duplicate,
// incomplete or cyclic inventories fail closed. Canonical results are retained;
// affected runs/tasks must be fenced before any replacement graph is admitted.
func PlanNegativeInvalidation(ctx context.Context,originTask,originRun uuid.UUID,route string,inventory []NegativeDependencyTask,maxTasks,maxEdges int)(NegativeInvalidationPlan,error) {
 var zero NegativeInvalidationPlan
 if err:=ctx.Err();err!=nil { return zero,err }
 if originTask==uuid.Nil||originRun==uuid.Nil||(route!="base"&&route!="skip")||maxTasks<1||maxTasks>100000||maxEdges<0||maxEdges>1000000||len(inventory)<1||len(inventory)>maxTasks { return zero,v2input.ErrInvalid }
 tasks:=make(map[uuid.UUID]NegativeDependencyTask,len(inventory));children:=make(map[uuid.UUID][]uuid.UUID);indegree:=make(map[uuid.UUID]int,len(inventory));edges:=0
 for _,entry:=range inventory {
  if err:=ctx.Err();err!=nil { return zero,err }
  if entry.TaskID==uuid.Nil||entry.RunID==uuid.Nil { return zero,v2input.ErrInvalid }
  if _,exists:=tasks[entry.TaskID];exists { return zero,v2input.ErrInvalid }
  entry.Parents=append([]uuid.UUID(nil),entry.Parents...);tasks[entry.TaskID]=entry
  if len(entry.Parents)>maxEdges-edges { return zero,v2input.ErrInvalid };edges+=len(entry.Parents)
 }
 origin,exists:=tasks[originTask];if !exists||origin.RunID!=originRun { return zero,v2input.ErrInvalid }
 for id,entry:=range tasks {
  seen:=make(map[uuid.UUID]bool,len(entry.Parents))
  for _,parent:=range entry.Parents {
   if _,exists:=tasks[parent];!exists||parent==id||seen[parent] { return zero,v2input.ErrInvalid }
   seen[parent]=true;children[parent]=append(children[parent],id);indegree[id]++
  }
 }
 // Validate all inventory components, not only the selected producer's branch.
 queue:=make([]uuid.UUID,0,len(tasks));for id:=range tasks { if indegree[id]==0 { queue=append(queue,id) } }
 visited:=0
 for cursor:=0;cursor<len(queue);cursor++ {
  if err:=ctx.Err();err!=nil { return zero,err };visited++
  for _,child:=range children[queue[cursor]] { indegree[child]--;if indegree[child]==0 { queue=append(queue,child) } }
 }
 if visited!=len(tasks) { return zero,v2input.ErrInvalid }
 affected:=map[uuid.UUID]bool{originTask:true};queue=[]uuid.UUID{originTask}
 for cursor:=0;cursor<len(queue);cursor++ {
  if err:=ctx.Err();err!=nil { return zero,err }
  for _,child:=range children[queue[cursor]] { if !affected[child] { affected[child]=true;queue=append(queue,child) } }
 }
 plan:=NegativeInvalidationPlan{SchemaVersion:1,OriginTask:originTask,OriginRun:originRun,Route:route,AffectedTasks:[]uuid.UUID{},ConsumerRuns:[]uuid.UUID{}}
 runs:=map[uuid.UUID]bool{}
 for task:=range affected {
  if task==originTask { continue }
  plan.AffectedTasks=append(plan.AffectedTasks,task)
  if tasks[task].RunID==originRun { plan.RecompileOrigin=true } else { runs[tasks[task].RunID]=true }
 }
 for run:=range runs { plan.ConsumerRuns=append(plan.ConsumerRuns,run) }
 sort.Slice(plan.AffectedTasks,func(i,j int)bool{return plan.AffectedTasks[i].String()<plan.AffectedTasks[j].String()})
 sort.Slice(plan.ConsumerRuns,func(i,j int)bool{return plan.ConsumerRuns[i].String()<plan.ConsumerRuns[j].String()})
 return plan,ctx.Err()
}
