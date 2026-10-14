package v2optical

import (
 "context"
 "reflect"
 "testing"
 "github.com/google/uuid"
)
func TestNegativeInvalidationIncludesTransitiveConsumersAndSameRunWork(t *testing.T) {
 a,b,c,d:=uuid.New(),uuid.New(),uuid.New(),uuid.New();run,consumer,unrelated:=uuid.New(),uuid.New(),uuid.New()
 tasks:=[]NegativeDependencyTask{{a,run,nil},{b,run,[]uuid.UUID{a}},{c,consumer,[]uuid.UUID{a,b}},{d,unrelated,nil}}
 for _,route:=range []string{"base","skip"} {
  plan,err:=PlanNegativeInvalidation(context.Background(),a,run,route,tasks,4,3)
  if err!=nil||len(plan.AffectedTasks)!=2||len(plan.ConsumerRuns)!=1||plan.ConsumerRuns[0]!=consumer||!plan.RecompileOrigin { t.Fatal("incomplete dependency closure",plan,err) }
  reversed:=[]NegativeDependencyTask{tasks[3],tasks[2],tasks[1],tasks[0]}
  again,err:=PlanNegativeInvalidation(context.Background(),a,run,route,reversed,4,3)
  if err!=nil||!reflect.DeepEqual(plan,again) { t.Fatal("plan depends on inventory order",err) }
  tasks[1].Parents[0]=d
  if len(plan.AffectedTasks)!=2 { t.Fatal("inventory mutation changed issued plan") };tasks[1].Parents[0]=a
 }
}
func TestNegativeInvalidationRejectsIncompleteCyclicOrUnboundedInventory(t *testing.T) {
 a,b,c:=uuid.New(),uuid.New(),uuid.New();run:=uuid.New()
 for _,mode:=range []string{"missing_parent","cycle","duplicate","self","foreign_origin","edge_budget","task_budget","route"} {
  tasks:=[]NegativeDependencyTask{{a,run,nil},{b,run,[]uuid.UUID{a}}};maxTasks,maxEdges,route:=2,2,"base"
  switch mode {
  case "missing_parent":tasks[1].Parents=[]uuid.UUID{c}
  case "cycle":tasks[0].Parents=[]uuid.UUID{b}
  case "duplicate":tasks[1].TaskID=a
  case "self":tasks[1].Parents=[]uuid.UUID{b}
  case "foreign_origin":tasks[0].RunID=uuid.New()
  case "edge_budget":maxEdges=0
  case "task_budget":maxTasks=1
  case "route":route="candidate"
  }
  if _,err:=PlanNegativeInvalidation(context.Background(),a,run,route,tasks,maxTasks,maxEdges);err==nil { t.Fatal("invalid graph accepted",mode) }
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=PlanNegativeInvalidation(ctx,a,run,"base",nil,2,2);err!=context.Canceled { t.Fatal("cancelled planner",err) }
}
