package workflow

import (
 "errors"
 "sort"
 "time"
 "github.com/google/uuid"
)

var capacityClasses = [...]string{"interactive","capture","batch","training"}

// CapacityCandidate must come from the leader's current eligible database snapshot.
// The selector cannot establish worker qualification, pool identity or SQL authority.
type CapacityCandidate struct {
 TaskID, TenantID uuid.UUID
 OriginalQueue string
 CreatedAt time.Time
 EstimatedMilliseconds *int64
 Heavy bool
 TenantQueuedHeavy int64
}

type CapacitySelection struct {
 TaskID uuid.UUID
 tenant uuid.UUID
 class string
 cost int64
 createdAt time.Time
}

// CapacityDRR belongs to one scheduling leader and one independently scheduled
// execution pool. It is intentionally not safe for concurrent callers.
type CapacityDRR struct {
 policy CapacityQueuePolicy
 deficits map[string]int64
 lastTenant uuid.UUID
 classCursor int
 pending *CapacitySelection
}

func NewCapacityDRR(policy CapacityQueuePolicy)(*CapacityDRR,error){
 if err:=policy.Validate();err!=nil{return nil,err}
 owned:=policy;owned.Weights=make(map[string]int64,4)
 for k,v:=range policy.Weights{owned.Weights[k]=v}
 return &CapacityDRR{policy:owned,deficits:make(map[string]int64,4)},nil
}

// BeginCycle grants one quantum per class, capped by max(3600,runnable cost).
// Snapshot validation completes before any deficit changes.
func (d *CapacityDRR) BeginCycle(candidates []CapacityCandidate,now time.Time)error{
 if now.IsZero(){return errors.New("invalid capacity cycle clock")}
 if d.pending!=nil{return errors.New("unsettled capacity selection")}
 capSeconds:=int64(3600)
 seen:=make(map[uuid.UUID]bool,len(candidates))
 for _,c:=range candidates{
  cost,_,err:=d.describe(c,now);if err!=nil{return err}
  if seen[c.TaskID]{return errors.New("duplicate capacity task")};seen[c.TaskID]=true
  if (!c.Heavy||c.TenantQueuedHeavy<d.policy.TenantHeavyQueued)&&cost>capSeconds{capSeconds=cost}
 }
 d.grantQuantum(capSeconds)
 return nil
}

func(d *CapacityDRR) grantQuantum(capSeconds int64){
 for _,class:=range capacityClasses{
  current:=d.deficits[class];if current>capSeconds{current=capSeconds}
  quantum:=d.policy.Weights[class]*30
  if current>capSeconds-quantum{current=capSeconds}else{current+=quantum}
  d.deficits[class]=current
 }
}

func(d *CapacityDRR) describe(c CapacityCandidate,now time.Time)(int64,string,error){
 if c.TaskID==uuid.Nil||c.TenantID==uuid.Nil||c.TenantQueuedHeavy<0||c.TenantQueuedHeavy>capacitySafeInteger{return 0,"",errors.New("invalid capacity candidate")}
 class,err:=CapacityEffectiveQueue(c.OriginalQueue,c.CreatedAt,now);if err!=nil{return 0,"",err}
 cost,err:=CapacityTaskCharge(c.EstimatedMilliseconds,d.policy.UnknownCostSeconds)
 return cost,class,err
}

// Next chooses FIFO within a tenant and round-robin tenants by UUID. Affinity
// cannot reorder candidates here. Refused candidates must be removed by caller.
func(d *CapacityDRR) Next(candidates []CapacityCandidate,now time.Time)(*CapacitySelection,error){
 if now.IsZero(){return nil,errors.New("invalid capacity cycle clock")}
 if d.pending!=nil{return nil,errors.New("unsettled capacity selection")}
 type item struct{c CapacityCandidate;cost int64;class string}
 type headKey struct{tenant uuid.UUID;class string}
 headsByKey:=make(map[headKey]item);seen:=make(map[uuid.UUID]bool,len(candidates))
 for _,c:=range candidates{
  cost,class,err:=d.describe(c,now);if err!=nil{return nil,err}
  if seen[c.TaskID]{return nil,errors.New("duplicate capacity task")};seen[c.TaskID]=true
  if c.Heavy&&c.TenantQueuedHeavy>=d.policy.TenantHeavyQueued{continue}
  key:=headKey{c.TenantID,class}
  previous,exists:=headsByKey[key]
  if !exists||c.CreatedAt.Before(previous.c.CreatedAt)||(c.CreatedAt.Equal(previous.c.CreatedAt)&&c.TaskID.String()<previous.c.TaskID.String()){headsByKey[key]=item{c,cost,class}}
 }
 // Only one FIFO head per tenant/effective class can participate.
 items:=make([]item,0,len(headsByKey))
 for _,head:=range headsByKey{items=append(items,head)}
 sort.Slice(items,func(i,j int)bool{return items[i].c.TenantID.String()<items[j].c.TenantID.String()})
 for offset:=0;offset<4;offset++{
  index:=(d.classCursor+offset)%4;class:=capacityClasses[index]
  // First after the cursor, then wrap. Only the oldest task of each tenant
  // participates, preventing a cheaper task from jumping its FIFO head.
  heads:=make([]item,0);tenants:=make(map[uuid.UUID]bool)
  for _,it:=range items{if it.class!=class||tenants[it.c.TenantID]{continue};tenants[it.c.TenantID]=true;heads=append(heads,it)}
  for pass:=0;pass<2;pass++{for _,it:=range heads{
   after:=it.c.TenantID.String()>d.lastTenant.String()
   if (pass==0&&!after)||(pass==1&&after)||it.cost>d.deficits[class]{continue}
   d.pending=&CapacitySelection{TaskID:it.c.TaskID,tenant:it.c.TenantID,class:class,cost:it.cost,createdAt:it.c.CreatedAt}
   return d.pending,nil
  }}
 }
 return nil,nil
}

// Settle is called with queued=true only after the SQL transaction committed.
// A failed/rolled-back reservation consumes no deficit and advances no cursor.
func(d *CapacityDRR) Settle(selection *CapacitySelection,queued bool)error{
 if selection==nil||selection!=d.pending{return errors.New("invalid capacity selection receipt")}
 if queued{
  d.deficits[selection.class]-=selection.cost
  d.lastTenant=selection.tenant
  for i,class:=range capacityClasses{if class==selection.class{d.classCursor=(i+1)%4;break}}
 }
 d.pending=nil
 return nil
}

// ApplyAffinity replaces only the current uncommitted receipt. Evidence must
// come from the current qualified database snapshot; this method grants no
// residency authority. Errors leave the receipt and all accounting unchanged.
func(d *CapacityDRR) ApplyAffinity(selection *CapacitySelection,candidates []CapacityCandidate,evidence []CapacityAffinityEvidence,now time.Time)(*CapacitySelection,error){
 if selection==nil||selection!=d.pending{return nil,errors.New("invalid capacity affinity receipt")}
 eligible:=make([]CapacityCandidate,0,len(candidates))
 known:=make(map[uuid.UUID]bool,len(candidates));allowed:=make(map[uuid.UUID]bool,len(candidates))
 costs:=make(map[uuid.UUID]int64,len(candidates))
 for _,c:=range candidates{
  cost,_,err:=d.describe(c,now);if err!=nil{return nil,err}
  if known[c.TaskID]{return nil,errors.New("duplicate capacity task")};known[c.TaskID]=true
  if c.Heavy&&c.TenantQueuedHeavy>=d.policy.TenantHeavyQueued{continue}
  eligible=append(eligible,c);allowed[c.TaskID]=true;costs[c.TaskID]=cost
 }
 filtered:=make([]CapacityAffinityEvidence,0,len(evidence));seen:=make(map[uuid.UUID]bool,len(evidence))
 for _,e:=range evidence{
  if !known[e.TaskID]||!capacityPolicyDigest.MatchString(e.ModelLockSHA)||seen[e.TaskID]{return nil,errors.New("invalid capacity affinity evidence")}
  seen[e.TaskID]=true
  if allowed[e.TaskID]{filtered=append(filtered,e)}
 }
 chosen,err:=SelectCapacityAffinity(eligible,filtered,selection.TaskID,now);if err!=nil{return nil,err}
 // Recheck the receipt scope and charge against the supplied current snapshot.
 var head CapacityCandidate
 for _,c:=range eligible{if c.TaskID==selection.TaskID{head=c;break}}
 _,class,err:=d.describe(head,now);if err!=nil{return nil,err}
 if head.TenantID!=selection.tenant||class!=selection.class||costs[selection.TaskID]!=selection.cost||!head.CreatedAt.Equal(selection.createdAt){return nil,errors.New("changed capacity affinity head")}
 if chosen==selection.TaskID||costs[chosen]>d.deficits[selection.class]{return selection,nil}
 var chosenCreated time.Time
 for _,c:=range eligible{if c.TaskID==chosen{chosenCreated=c.CreatedAt;break}}
 replacement:=&CapacitySelection{TaskID:chosen,tenant:selection.tenant,class:selection.class,cost:costs[chosen],createdAt:chosenCreated}
 d.pending=replacement
 return replacement,nil
}
