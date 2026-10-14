package workflow

import (
 "testing"
 "time"
 "github.com/google/uuid"
)

func capacityDRRFixture(t *testing.T)(*CapacityDRR,time.Time){
 t.Helper();d,err:=NewCapacityDRR(CapacityQueuePolicy{Weights:map[string]int64{"interactive":4,"capture":3,"batch":2,"training":1},UnknownCostSeconds:1800,TenantHeavyQueued:2});if err!=nil{t.Fatal(err)}
 return d,time.Date(2026,10,12,0,0,0,0,time.UTC)
}
func TestCapacityDRRChargesOnlyCommittedSelection(t *testing.T){
 d,now:=capacityDRRFixture(t);ms:=int64(1000)
 c:=CapacityCandidate{TaskID:uuid.New(),TenantID:uuid.New(),OriginalQueue:"interactive",CreatedAt:now,EstimatedMilliseconds:&ms}
 if err:=d.BeginCycle([]CapacityCandidate{c},now);err!=nil{t.Fatal(err)}
 selection,err:=d.Next([]CapacityCandidate{c},now);if err!=nil||selection==nil{t.Fatal(err)}
 if d.BeginCycle(nil,now)==nil{t.Fatal("accepted cycle with pending dispatch")}
 if err=d.Settle(selection,false);err!=nil||d.deficits["interactive"]!=120||d.lastTenant!=uuid.Nil{t.Fatal("refusal consumed budget",err)}
 selection,err=d.Next([]CapacityCandidate{c},now);if err!=nil||selection==nil{t.Fatal(err)}
 if err=d.Settle(selection,true);err!=nil||d.deficits["interactive"]!=119||d.lastTenant!=c.TenantID{t.Fatal("wrong committed charge",err)}
 if d.Settle(selection,true)==nil{t.Fatal("accepted duplicate settlement")}
}
func TestCapacityDRRFIFOHeadCannotBeBypassed(t *testing.T){
 d,now:=capacityDRRFixture(t);tenant:=uuid.New();expensive,cheap:=int64(200000),int64(1000)
 oldest:=CapacityCandidate{TaskID:uuid.New(),TenantID:tenant,OriginalQueue:"interactive",CreatedAt:now.Add(-time.Second),EstimatedMilliseconds:&expensive}
 newer:=oldest;newer.TaskID=uuid.New();newer.CreatedAt=now;newer.EstimatedMilliseconds=&cheap
 candidates:=[]CapacityCandidate{newer,oldest}
 if err:=d.BeginCycle(candidates,now);err!=nil{t.Fatal(err)}
 if selected,err:=d.Next(candidates,now);err!=nil||selected!=nil{t.Fatal("bypassed oldest",err)}
 if err:=d.BeginCycle(candidates,now);err!=nil{t.Fatal(err)}
 selected,err:=d.Next(candidates,now);if err!=nil||selected==nil||selected.TaskID!=oldest.TaskID{t.Fatal("oldest not selected",err)}
}
func TestCapacityDRRUnknownCostEventuallyFitsAndCapIsBounded(t *testing.T){
 d,now:=capacityDRRFixture(t)
 c:=CapacityCandidate{TaskID:uuid.New(),TenantID:uuid.New(),OriginalQueue:"training",CreatedAt:now}
 for i:=0;i<60;i++{if err:=d.BeginCycle([]CapacityCandidate{c},now);err!=nil{t.Fatal(err)}}
 selected,err:=d.Next([]CapacityCandidate{c},now);if err!=nil||selected==nil{t.Fatal("unknown task starved",err)}
 if err=d.Settle(selected,true);err!=nil||d.deficits["training"]!=0{t.Fatal(err)}
 for i:=0;i<200;i++{if err=d.BeginCycle(nil,now);err!=nil{t.Fatal(err)}}
 for _,value:=range d.deficits{if value>3600{t.Fatal("unbounded deficit",value)}}
}
func TestCapacityDRRRejectsInvalidSnapshotAtomically(t *testing.T){
 d,now:=capacityDRRFixture(t)
 c:=CapacityCandidate{TaskID:uuid.New(),TenantID:uuid.New(),OriginalQueue:"batch",CreatedAt:now}
 if d.BeginCycle([]CapacityCandidate{c,c},now)==nil{t.Fatal("accepted duplicate task")}
 for _,v:=range d.deficits{if v!=0{t.Fatal("partial quantum on invalid snapshot")}}
 c.Heavy=true;c.TenantQueuedHeavy=2
 if err:=d.BeginCycle([]CapacityCandidate{c},now);err!=nil{t.Fatal(err)}
 selected,err:=d.Next([]CapacityCandidate{c},now);if err!=nil||selected!=nil{t.Fatal("tenant heavy cap bypassed",err)}
}

func TestCapacityDRRTenantRoundRobinAndOwnedPolicy(t *testing.T){
 policy:=CapacityQueuePolicy{Weights:map[string]int64{"interactive":4,"capture":3,"batch":2,"training":1},UnknownCostSeconds:1800,TenantHeavyQueued:2}
 d,err:=NewCapacityDRR(policy);if err!=nil{t.Fatal(err)}
 policy.Weights["interactive"]=100
 now:=time.Date(2026,10,12,0,0,0,0,time.UTC);ms:=int64(1000)
 low:=uuid.MustParse("00000000-0000-0000-0000-000000000001")
 high:=uuid.MustParse("00000000-0000-0000-0000-000000000002")
 a:=CapacityCandidate{TaskID:uuid.New(),TenantID:low,OriginalQueue:"interactive",CreatedAt:now,EstimatedMilliseconds:&ms}
 b:=a;b.TaskID=uuid.New();b.CreatedAt=now.Add(time.Second)
 c:=a;c.TaskID=uuid.New();c.TenantID=high
 candidates:=[]CapacityCandidate{b,c,a}
 if err=d.BeginCycle(candidates,now.Add(time.Second));err!=nil{t.Fatal(err)}
 if d.deficits["interactive"]!=120{t.Fatal("caller mutated installed policy")}
 first,err:=d.Next(candidates,now.Add(time.Second));if err!=nil||first==nil||first.TaskID!=a.TaskID{t.Fatal("wrong initial FIFO tenant",err)}
 if err=d.Settle(first,true);err!=nil{t.Fatal(err)}
 second,err:=d.Next([]CapacityCandidate{b,c},now.Add(time.Second));if err!=nil||second==nil||second.TaskID!=c.TaskID{t.Fatal("tenant cursor did not advance",err)}
 if err=d.Settle(second,true);err!=nil{t.Fatal(err)}
 third,err:=d.Next([]CapacityCandidate{b},now.Add(time.Second));if err!=nil||third==nil||third.TaskID!=b.TaskID{t.Fatal("tenant cursor did not wrap",err)}
}

// A large early-tenant backlog must not exclude a later tenant from selection.
func TestCapacityDRRFullBacklogRetainsLaterTenant(t *testing.T) {
 policy := CapacityQueuePolicy{Weights: map[string]int64{"interactive":1,"capture":1,"batch":1,"training":1}, UnknownCostSeconds:1,TenantHeavyQueued:10}
 d, err := NewCapacityDRR(policy)
 if err != nil { t.Fatal(err) }
 now := time.Now()
 first := uuid.MustParse("00000000-0000-0000-0000-000000000001")
 last := uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")
 candidates := make([]CapacityCandidate,0,10002)
 for i:=0;i<10001;i++ { candidates=append(candidates,CapacityCandidate{TaskID:uuid.New(),TenantID:first,OriginalQueue:"batch",CreatedAt:now.Add(-time.Second)}) }
 final := CapacityCandidate{TaskID:uuid.New(),TenantID:last,OriginalQueue:"batch",CreatedAt:now.Add(-time.Second)}
 candidates=append(candidates,final)
 if err:=d.BeginCycle(candidates,now);err!=nil { t.Fatal(err) }
 one,err:=d.Next(candidates,now)
 if err!=nil||one==nil { t.Fatal("first tenant selection",err) }
 if err:=d.Settle(one,true);err!=nil { t.Fatal(err) }
 filtered:=make([]CapacityCandidate,0,len(candidates)-1)
 for _,c:=range candidates { if c.TaskID!=one.TaskID { filtered=append(filtered,c) } }
 two,err:=d.Next(filtered,now)
 if err!=nil||two==nil||two.TaskID!=final.TaskID { t.Fatal("later tenant excluded by large backlog",err) }
}

func TestCapacityDRRHeadCompressionPreservesFIFOAndEffectiveClasses(t *testing.T){
 d,now:=capacityDRRFixture(t)
 tenant:=uuid.New()
 expensive:=int64(3000000)
 cheap:=int64(1000)
 older:=CapacityCandidate{TaskID:uuid.New(),TenantID:tenant,OriginalQueue:"batch",CreatedAt:now.Add(-time.Minute),EstimatedMilliseconds:&expensive}
 newer:=CapacityCandidate{TaskID:uuid.New(),TenantID:tenant,OriginalQueue:"batch",CreatedAt:now.Add(-time.Second),EstimatedMilliseconds:&cheap}
 if err:=d.BeginCycle([]CapacityCandidate{newer,older},now);err!=nil{t.Fatal(err)}
 if got,err:=d.Next([]CapacityCandidate{newer,older},now);err!=nil||got!=nil{t.Fatal("cheaper task bypassed FIFO",got,err)}
 // Aging creates a distinct effective queue, so this head remains selectable.
 promoted:=CapacityCandidate{TaskID:uuid.New(),TenantID:tenant,OriginalQueue:"batch",CreatedAt:now.Add(-11*time.Minute),EstimatedMilliseconds:&cheap}
 got,err:=d.Next([]CapacityCandidate{newer,promoted,older},now)
 if err!=nil||got==nil||got.TaskID!=promoted.TaskID{t.Fatal("promoted class head lost",got,err)}
 if err:=d.Settle(got,false);err!=nil{t.Fatal(err)}
 // A duplicate remains invalid even if it would be discarded as a later head.
 if got,err:=d.Next([]CapacityCandidate{older,newer,newer},now);err==nil||got!=nil{t.Fatal("duplicate hidden by compression")}
}

func TestCapacityDRRAffinityReceiptAndAccounting(t *testing.T){
 for _,commit:=range []bool{false,true}{t.Run(map[bool]string{false:"refused",true:"committed"}[commit],func(t *testing.T){
  d,now:=capacityDRRFixture(t);cheap,cost:=int64(1000),int64(7000)
  head:=CapacityCandidate{TaskID:uuid.New(),TenantID:uuid.New(),OriginalQueue:"interactive",CreatedAt:now.Add(-30*time.Second),EstimatedMilliseconds:&cheap}
  resident:=head;resident.TaskID=uuid.New();resident.CreatedAt=now;resident.EstimatedMilliseconds=&cost
  candidates:=[]CapacityCandidate{head,resident}
  if err:=d.BeginCycle(candidates,now);err!=nil{t.Fatal(err)}
  original,err:=d.Next(candidates,now);if err!=nil||original==nil{t.Fatal(err)}
  evidence:=[]CapacityAffinityEvidence{{TaskID:resident.TaskID,ModelLockSHA:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",Resident:true}}
  selected,err:=d.ApplyAffinity(original,candidates,evidence,now);if err!=nil||selected==nil||selected.TaskID!=resident.TaskID{t.Fatal("affinity not selected",err)}
  if !selected.createdAt.Equal(resident.CreatedAt){t.Fatal("replacement lost FIFO timestamp")}
  if d.Settle(original,true)==nil{t.Fatal("obsolete receipt accepted")}
  if err=d.Settle(selected,commit);err!=nil{t.Fatal(err)}
  expected:=int64(120);if commit{expected-=7}
  if d.deficits["interactive"]!=expected{t.Fatal("wrong actual task charge")}
  if !commit&&d.lastTenant!=uuid.Nil{t.Fatal("refusal moved tenant cursor")}
 })}
}

func TestCapacityDRRAffinityRefusalsAreAtomic(t *testing.T){
 for _,mode:=range []string{"expensive","heavy","foreign","changed","changed-created"}{t.Run(mode,func(t *testing.T){
  d,now:=capacityDRRFixture(t);cheap,cost:=int64(1000),int64(200000)
  head:=CapacityCandidate{TaskID:uuid.New(),TenantID:uuid.New(),OriginalQueue:"interactive",CreatedAt:now.Add(-time.Second),EstimatedMilliseconds:&cheap}
  next:=head;next.TaskID=uuid.New();next.CreatedAt=now
  if mode=="expensive"{next.EstimatedMilliseconds=&cost}
  if mode=="heavy"{next.Heavy=true;next.TenantQueuedHeavy=2}
  candidates:=[]CapacityCandidate{head,next}
  if err:=d.BeginCycle(candidates,now);err!=nil{t.Fatal(err)}
  original,err:=d.Next(candidates,now);if err!=nil||original==nil{t.Fatal(err)}
  evidence:=[]CapacityAffinityEvidence{{TaskID:next.TaskID,ModelLockSHA:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",Resident:true}}
  if mode=="foreign"{evidence[0].TaskID=uuid.New()}
  if mode=="changed"{candidates[0].EstimatedMilliseconds=&cost}
  if mode=="changed-created"{candidates[0].CreatedAt=candidates[0].CreatedAt.Add(-time.Second)}
  selected,err:=d.ApplyAffinity(original,candidates,evidence,now)
  invalid:=mode=="foreign"||mode=="changed"||mode=="changed-created"
  if invalid&&err==nil{t.Fatal("invalid snapshot accepted")}
  if !invalid&&(err!=nil||selected!=original){t.Fatal("ineligible replacement selected",err)}
  if d.pending!=original||d.deficits["interactive"]!=120||d.lastTenant!=uuid.Nil{t.Fatal("refusal changed accounting")}
  if err=d.Settle(original,false);err!=nil{t.Fatal(err)}
 })}
}

func TestCapacityInvalidSnapshotsPreserveExistingAccounting(t *testing.T){
 for _,mode:=range []string{"task","tenant","queue","future","negative cost","oversized cost","negative heavy","oversized heavy","duplicate","zero clock","empty zero clock"}{t.Run(mode,func(t *testing.T){
  d,now:=capacityDRRFixture(t)
  d.deficits["interactive"]=57;d.deficits["batch"]=91;d.lastTenant=uuid.New();d.classCursor=2
  previousTenant:=d.lastTenant
  value:=int64(1000)
  candidate:=CapacityCandidate{TaskID:uuid.New(),TenantID:uuid.New(),OriginalQueue:"batch",CreatedAt:now,EstimatedMilliseconds:&value}
  candidates:=[]CapacityCandidate{candidate}
  switch mode{
  case "task":candidates[0].TaskID=uuid.Nil
  case "tenant":candidates[0].TenantID=uuid.Nil
  case "queue":candidates[0].OriginalQueue="unknown"
  case "future":candidates[0].CreatedAt=now.Add(time.Second)
  case "negative cost":value=-1
  case "oversized cost":value=capacitySafeInteger+1
  case "negative heavy":candidates[0].TenantQueuedHeavy=-1
  case "oversized heavy":candidates[0].TenantQueuedHeavy=capacitySafeInteger+1
  case "duplicate":candidates=append(candidates,candidate)
  case "zero clock":now=time.Time{}
  case "empty zero clock":candidates=nil;now=time.Time{}
  }
  if err:=d.BeginCycle(candidates,now);err==nil{t.Fatal("invalid cycle accepted")}
  if selected,err:=d.Next(candidates,now);err==nil||selected!=nil{t.Fatal("invalid selection accepted")}
  if d.deficits["interactive"]!=57||d.deficits["batch"]!=91||d.deficits["capture"]!=0||d.deficits["training"]!=0||d.lastTenant!=previousTenant||d.classCursor!=2||d.pending!=nil{t.Fatal("invalid snapshot changed accounting")}
 })}
}
