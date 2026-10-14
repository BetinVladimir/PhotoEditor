package workflow

import (
 "strings"
 "testing"
 "time"

 "github.com/google/uuid"
)

func TestCapacityHeadCycleUsesFullSnapshotCapAndSettlesReplacement(t *testing.T){
 policy:=CapacityQueuePolicy{Weights:map[string]int64{"interactive":4,"capture":3,"batch":2,"training":1},UnknownCostSeconds:1800,TenantHeavyQueued:2}
 d,err:=NewCapacityDRR(policy);if err!=nil{t.Fatal(err)}
 now:=time.Date(2026,10,13,12,0,0,0,time.UTC)
 head:=CapacityCandidate{TaskID:uuid.New(),TenantID:uuid.New(),OriginalQueue:"interactive",CreatedAt:now.Add(-time.Minute)}
 if err=d.BeginHeadCycle(CapacityHeadSnapshot{Heads:[]CapacityCandidate{head},MaxRunnableCostSeconds:5000},now);err!=nil{t.Fatal(err)}
 if d.deficits["interactive"]!=120{t.Fatal("wrong initial quantum",d.deficits)}
 // Accumulation remains capped by a cost from a task absent from the head set.
 for i:=0;i<50;i++{if err=d.BeginHeadCycle(CapacityHeadSnapshot{Heads:[]CapacityCandidate{head},MaxRunnableCostSeconds:5000},now);err!=nil{t.Fatal(err)}}
 if d.deficits["interactive"]!=5000{t.Fatal("lost full snapshot cap",d.deficits)}
 selection,err:=d.Next([]CapacityCandidate{head},now);if err!=nil||selection==nil{t.Fatal(selection,err)}
 newer:=head;newer.TaskID=uuid.New();newer.CreatedAt=head.CreatedAt.Add(30*time.Second)
 hint:=CapacityHeadAffinity{Candidate:newer,ModelLockSHA:strings.Repeat("a",64),Resident:true}
 replacement,err:=d.ApplyHeadAffinity(selection,head,hint,now);if err!=nil||replacement==selection||replacement.TaskID!=newer.TaskID{t.Fatal(replacement,err)}
 if err=d.Settle(selection,true);err==nil{t.Fatal("stale receipt settled")}
 if err=d.Settle(replacement,true);err!=nil{t.Fatal(err)}
 if d.deficits["interactive"]!=3200{t.Fatal("wrong committed replacement charge",d.deficits)}
}

func TestCapacityHeadAffinityRefusalDoesNotChangePending(t *testing.T){
 policy:=CapacityQueuePolicy{Weights:map[string]int64{"interactive":4,"capture":3,"batch":2,"training":1},UnknownCostSeconds:100,TenantHeavyQueued:2}
 d,err:=NewCapacityDRR(policy);if err!=nil{t.Fatal(err)}
 now:=time.Date(2026,10,13,12,0,0,0,time.UTC)
 head:=CapacityCandidate{TaskID:uuid.New(),TenantID:uuid.New(),OriginalQueue:"interactive",CreatedAt:now.Add(-time.Minute)}
 if err=d.BeginHeadCycle(CapacityHeadSnapshot{Heads:[]CapacityCandidate{head},MaxRunnableCostSeconds:3600},now);err!=nil{t.Fatal(err)}
 selection,err:=d.Next([]CapacityCandidate{head},now);if err!=nil||selection==nil{t.Fatal(selection,err)}
 later:=head;later.TaskID=uuid.New();later.CreatedAt=head.CreatedAt.Add(31*time.Second)
 if _,err=d.ApplyHeadAffinity(selection,head,CapacityHeadAffinity{Candidate:later,ModelLockSHA:strings.Repeat("a",64),Resident:true},now);err==nil||d.pending!=selection{t.Fatal("late affinity changed receipt",err)}
 changed:=head;estimated:=int64(200000);changed.EstimatedMilliseconds=&estimated
 if _,err=d.ApplyHeadAffinity(selection,head,CapacityHeadAffinity{Candidate:changed,ModelLockSHA:strings.Repeat("a",64),Resident:true},now);err==nil||d.pending!=selection{t.Fatal("changed resident head cost accepted",err)}
 if got,err:=d.ApplyHeadAffinity(selection,head,CapacityHeadAffinity{Candidate:head},now);err!=nil||got!=selection{t.Fatal(got,err)}
 if err=d.Settle(selection,false);err!=nil{t.Fatal(err)}
 if d.deficits["interactive"]!=120{t.Fatal("failed dispatch charged",d.deficits)}
}

func TestCapacityHeadCycleMatchesFullCandidateDRR(t *testing.T){
 policy:=CapacityQueuePolicy{Weights:map[string]int64{"interactive":4,"capture":3,"batch":2,"training":1},UnknownCostSeconds:1800,TenantHeavyQueued:2}
 full,err:=NewCapacityDRR(policy);if err!=nil{t.Fatal(err)}
 compact,err:=NewCapacityDRR(policy);if err!=nil{t.Fatal(err)}
 legacy,err:=NewCapacityDRR(policy);if err!=nil{t.Fatal(err)}
 now:=time.Date(2026,10,13,12,0,0,0,time.UTC)
 one:=int64(1000);expensive:=int64(4_800_000)
 a,b,c,d:=uuid.New(),uuid.New(),uuid.New(),uuid.New()
 candidates:=[]CapacityCandidate{
  {TaskID:uuid.New(),TenantID:a,OriginalQueue:"interactive",CreatedAt:now.Add(-time.Minute),EstimatedMilliseconds:&one},
  {TaskID:uuid.New(),TenantID:a,OriginalQueue:"interactive",CreatedAt:now.Add(-30*time.Second),EstimatedMilliseconds:&expensive},
  {TaskID:uuid.New(),TenantID:b,OriginalQueue:"capture",CreatedAt:now.Add(-time.Minute),EstimatedMilliseconds:&one},
  {TaskID:uuid.New(),TenantID:c,OriginalQueue:"training",CreatedAt:now.Add(-11*time.Minute),EstimatedMilliseconds:&one},
  {TaskID:uuid.New(),TenantID:d,OriginalQueue:"interactive",CreatedAt:now.Add(-time.Minute),EstimatedMilliseconds:&one,Heavy:true,TenantQueuedHeavy:2},
 }
 heads:=[]CapacityCandidate{candidates[0],candidates[2],candidates[3]}
 snapshot:=CapacityHeadSnapshot{Heads:heads,MaxRunnableCostSeconds:4800}
 for cycle:=0;cycle<60;cycle++{
  if err=full.BeginCycle(candidates,now);err!=nil{t.Fatal(cycle,err)}
  if err=compact.BeginHeadCycle(snapshot,now);err!=nil{t.Fatal(cycle,err)}
  if err=legacy.BeginHeadCycle(snapshot,now);err!=nil{t.Fatal(cycle,err)}
  expected,e:=full.Next(candidates,now);if e!=nil{t.Fatal(cycle,e)}
  actual,e:=compact.nextHead(heads,now);if e!=nil{t.Fatal(cycle,e)}
  previous,e:=legacy.Next(heads,now);if e!=nil{t.Fatal(cycle,e)}
  if (previous==nil)!=(actual==nil)||previous!=nil&&previous.TaskID!=actual.TaskID{t.Fatal("head selector diverged from previous order",cycle,previous,actual)}
  if (expected==nil)!=(actual==nil)||expected!=nil&&(expected.TaskID!=actual.TaskID||expected.tenant!=actual.tenant||expected.class!=actual.class||expected.cost!=actual.cost){t.Fatal("head snapshot changed DRR order",cycle,expected,actual)}
  if expected!=nil{
   if err=full.Settle(expected,true);err!=nil{t.Fatal(err)}
   if err=compact.Settle(actual,true);err!=nil{t.Fatal(err)}
   if err=legacy.Settle(previous,true);err!=nil{t.Fatal(err)}
  }
  for _,class:=range capacityClasses{if full.deficits[class]!=compact.deficits[class]{t.Fatal("head snapshot changed deficit",cycle,class)}}
 }
}

func TestCapacityHeadSinglePassKeepsClassAndTenantWrapOrder(t *testing.T){
 policy:=CapacityQueuePolicy{Weights:map[string]int64{"interactive":4,"capture":3,"batch":2,"training":1},UnknownCostSeconds:30,TenantHeavyQueued:2}
 now:=time.Date(2026,10,13,12,0,0,0,time.UTC)
 tenants:=[]uuid.UUID{uuid.MustParse("10000000-0000-4000-8000-000000000001"),uuid.MustParse("20000000-0000-4000-8000-000000000002"),uuid.MustParse("30000000-0000-4000-8000-000000000003")}
 heads:=make([]CapacityCandidate,0,12)
 for _,tenant:=range tenants{for _,class:=range capacityClasses{
  heads=append(heads,CapacityCandidate{TaskID:uuid.New(),TenantID:tenant,OriginalQueue:class,CreatedAt:now.Add(-time.Minute)})
 }}
 // Reverse SQL order so the selector cannot rely on the first candidate.
 for i,j:=0,len(heads)-1;i<j;i,j=i+1,j-1{heads[i],heads[j]=heads[j],heads[i]}
 for cursor:=0;cursor<len(capacityClasses);cursor++{for _,last:=range append([]uuid.UUID{uuid.Nil},tenants...){
  previous,err:=NewCapacityDRR(policy);if err!=nil{t.Fatal(err)}
  current,err:=NewCapacityDRR(policy);if err!=nil{t.Fatal(err)}
  previous.classCursor=cursor;current.classCursor=cursor
  previous.lastTenant=last;current.lastTenant=last
  for _,class:=range capacityClasses{previous.deficits[class]=30;current.deficits[class]=30}
  want,err:=previous.Next(heads,now);if err!=nil{t.Fatal(err)}
  got,err:=current.nextHead(heads,now);if err!=nil{t.Fatal(err)}
  if want==nil||got==nil||want.TaskID!=got.TaskID||want.tenant!=got.tenant||want.class!=got.class||want.cost!=got.cost{
   t.Fatal("single-pass DRR order changed",cursor,last,want,got)
  }
 }}
}
