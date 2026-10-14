package v2optical

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

func TestNegativeReplacementFrontierDiamondAndRecovery(t *testing.T){
 ctx:=context.Background();origin,a,b,c,retained,run:=uuid.New(),uuid.New(),uuid.New(),uuid.New(),uuid.New(),uuid.New()
 inventory:=[]NegativeDependencyTask{
  {TaskID:origin,RunID:run},{TaskID:retained,RunID:run},
  {TaskID:a,RunID:run,Parents:[]uuid.UUID{origin}},
  {TaskID:b,RunID:run,Parents:[]uuid.UUID{origin,retained}},
  {TaskID:c,RunID:run,Parents:[]uuid.UUID{a,b}},
 }
 plan,err:=PlanNegativeInvalidation(ctx,origin,run,"base",inventory,4096,65536);if err!=nil{t.Fatal(err)}
 source:=&v2input.Ref{ArtifactID:uuid.New(),SHA256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
 topology,err:=CompileNegativeReplacementTopology(ctx,uuid.New(),uuid.New(),uuid.New(),source.SHA256,plan,inventory,source,4096,65536);if err!=nil{t.Fatal(err)}
 ids:=map[uuid.UUID]uuid.UUID{};for _,node:=range topology.Nodes{ids[node.OriginalTask]=node.Task}
 first,err:=CompileNegativeReplacementFrontier(ctx,topology,plan,inventory,nil);if err!=nil||len(first)!=2{t.Fatal("independent roots",err,first)}
 for _,node:=range first{if node.OriginalTask==c{t.Fatal("join compiled before physical parents")}}
 next,err:=CompileNegativeReplacementFrontier(ctx,topology,plan,inventory,[]uuid.UUID{ids[a]});if err!=nil||len(next)!=1||next[0].OriginalTask!=b{t.Fatal("partial wave recovery",err,next)}
 join,err:=CompileNegativeReplacementFrontier(ctx,topology,plan,inventory,[]uuid.UUID{ids[b],ids[a]});if err!=nil||len(join)!=1||join[0].OriginalTask!=c{t.Fatal("join readiness",err,join)}
 join[0].Parents[0].Task=uuid.Nil
 if topology.Nodes[len(topology.Nodes)-1].Parents[0].Task==uuid.Nil{t.Fatal("returned parents alias topology")}
 end,err:=CompileNegativeReplacementFrontier(ctx,topology,plan,inventory,[]uuid.UUID{ids[c],ids[a],ids[b]});if err!=nil||len(end)!=0{t.Fatal("completed branch",err)}
 for _,bad:=range [][]uuid.UUID{{ids[c]},{ids[a],ids[a]},{origin},{retained},{uuid.New()}}{
  if _,err:=CompileNegativeReplacementFrontier(ctx,topology,plan,inventory,bad);err==nil{t.Fatal("invalid completion receipt set accepted",bad)}
 }
 topology.Nodes[0].Run=uuid.New()
 if _,err:=CompileNegativeReplacementFrontier(ctx,topology,plan,inventory,nil);err==nil{t.Fatal("tampered topology accepted")}
 cancelled,cancel:=context.WithCancel(ctx);cancel()
 if _,err:=CompileNegativeReplacementFrontier(cancelled,topology,plan,inventory,nil);err!=context.Canceled{t.Fatal("cancellation",err)}
}
