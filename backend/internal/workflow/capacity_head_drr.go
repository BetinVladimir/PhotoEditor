package workflow

import (
 "errors"
 "time"

 "github.com/google/uuid"
)

// nextHead selects from a BeginHeadCycle-validated one-head-per-tenant/class
// snapshot. It preserves Next's class cursor, tenant wrap and deficit rules
// without rebuilding or sorting the same head inventory on every reservation.
func(d *CapacityDRR) nextHead(heads []CapacityCandidate,now time.Time)(*CapacitySelection,error){
 if d==nil||now.IsZero()||d.pending!=nil{return nil,errors.New("invalid capacity head selection")}
 last:=d.lastTenant.String()
 // Keep the first tenant after the cursor and the wrap candidate for each
 // class in one pass. No class may be selected before every head is checked:
 // a later row can still contain a smaller tenant for that class.
 var after,wrap [len(capacityClasses)]*CapacitySelection
 for _,candidate:=range heads{
  cost,class,err:=d.describe(candidate,now);if err!=nil{return nil,err}
  if candidate.Heavy&&candidate.TenantQueuedHeavy>=d.policy.TenantHeavyQueued||cost>d.deficits[class]{continue}
  index:=-1
  for i,name:=range capacityClasses{if name==class{index=i;break}}
  if index<0{return nil,errors.New("invalid capacity head class")}
  tenant:=candidate.TenantID.String()
  target:=&wrap[index]
  if tenant>last{target=&after[index]}
  if *target==nil||tenant<(*target).tenant.String(){
   *target=&CapacitySelection{TaskID:candidate.TaskID,tenant:candidate.TenantID,class:class,cost:cost,createdAt:candidate.CreatedAt}
  }
 }
 for offset:=0;offset<len(capacityClasses);offset++{
  index:=(d.classCursor+offset)%len(capacityClasses)
  chosen:=after[index];if chosen==nil{chosen=wrap[index]}
  if chosen!=nil{d.pending=chosen;return chosen,nil}
 }
 return nil,nil
}

// BeginHeadCycle consumes SQL-compressed FIFO heads while retaining the maximum
// runnable charge of the entire eligible snapshot. It changes no deficits if
// any head or aggregate is malformed.
func(d *CapacityDRR) BeginHeadCycle(snapshot CapacityHeadSnapshot,now time.Time)error{
 if d==nil||now.IsZero()||d.pending!=nil{return errors.New("invalid capacity head cycle")}
 if snapshot.MaxRunnableCostSeconds<3600||snapshot.MaxRunnableCostSeconds>capacitySafeInteger{return errors.New("invalid capacity head cap")}
 type scope struct{tenant uuid.UUID;class string}
 seen:=make(map[scope]bool,len(snapshot.Heads));ids:=make(map[uuid.UUID]bool,len(snapshot.Heads))
 for _,c:=range snapshot.Heads{
  cost,class,err:=d.describe(c,now);if err!=nil{return err}
  key:=scope{c.TenantID,class}
  if seen[key]||ids[c.TaskID]||c.Heavy&&c.TenantQueuedHeavy>=d.policy.TenantHeavyQueued||cost>snapshot.MaxRunnableCostSeconds{return errors.New("invalid capacity FIFO head snapshot")}
  seen[key]=true;ids[c.TaskID]=true
 }
 d.grantQuantum(snapshot.MaxRunnableCostSeconds)
 return nil
}

// BeginBoundedHeadCycle grants the complete SQL-computed cap without retaining
// the per-tenant heads in Go. The SQL selector is re-read for every reservation.
func(d *CapacityDRR) BeginBoundedHeadCycle(capSeconds int64,now time.Time)error{
 if d==nil||now.IsZero()||d.pending!=nil||capSeconds<3600||capSeconds>capacitySafeInteger{return errors.New("invalid bounded capacity cycle")}
 d.grantQuantum(capSeconds)
 return nil
}

func(d *CapacityDRR) AcceptBoundedHead(choice CapacityBoundedHeadChoice,now time.Time)(*CapacitySelection,error){
 if d==nil||now.IsZero()||d.pending!=nil{return nil,errors.New("invalid bounded capacity selection")}
 if choice.Head==nil{return nil,nil}
 head:=*choice.Head
 cost,class,err:=d.describe(head,now)
 if err!=nil||class!=choice.EffectiveClass||head.Heavy&&head.TenantQueuedHeavy>=d.policy.TenantHeavyQueued||cost>d.deficits[class]||cost>choice.MaxRunnableCostSeconds{
  return nil,errors.New("invalid bounded capacity head")
 }
 d.pending=&CapacitySelection{TaskID:head.TaskID,tenant:head.TenantID,class:class,cost:cost,createdAt:head.CreatedAt}
 return d.pending,nil
}

// ApplyHeadAffinity replaces only the uncommitted receipt and only with one
// independently validated current resident hint in the same 30-second window.
// If that hint exceeds the class deficit, the FIFO head remains selected.
func(d *CapacityDRR) ApplyHeadAffinity(selection *CapacitySelection,head CapacityCandidate,hint CapacityHeadAffinity,now time.Time)(*CapacitySelection,error){
 if d==nil||selection==nil||selection!=d.pending||now.IsZero(){return nil,errors.New("invalid capacity head affinity receipt")}
 headCost,headClass,err:=d.describe(head,now);if err!=nil{return nil,err}
 if head.TaskID!=selection.TaskID||head.TenantID!=selection.tenant||headClass!=selection.class||headCost!=selection.cost||!head.CreatedAt.Equal(selection.createdAt)||head.Heavy&&head.TenantQueuedHeavy>=d.policy.TenantHeavyQueued{return nil,errors.New("changed capacity FIFO head")}
 candidate:=hint.Candidate
 cost,class,err:=d.describe(candidate,now);if err!=nil{return nil,err}
 if candidate.TenantID!=selection.tenant||class!=selection.class||candidate.Heavy&&candidate.TenantQueuedHeavy>=d.policy.TenantHeavyQueued{return nil,errors.New("foreign capacity resident hint")}
 if !hint.Resident{
  if hint.ModelLockSHA!=""||candidate.TaskID!=head.TaskID||!candidate.CreatedAt.Equal(head.CreatedAt)||cost!=headCost{return nil,errors.New("changed capacity FIFO fallback")}
  return selection,nil
 }
 if !capacityPolicyDigest.MatchString(hint.ModelLockSHA)||candidate.CreatedAt.Before(head.CreatedAt)||candidate.CreatedAt.Sub(head.CreatedAt)>30*time.Second||candidate.CreatedAt.Equal(head.CreatedAt)&&candidate.TaskID.String()<head.TaskID.String(){return nil,errors.New("invalid capacity resident window")}
 if candidate.TaskID==head.TaskID{
  if cost!=headCost||!candidate.CreatedAt.Equal(head.CreatedAt){return nil,errors.New("changed resident FIFO head")}
  return selection,nil
 }
 if cost>d.deficits[selection.class]{return selection,nil}
 replacement:=&CapacitySelection{TaskID:candidate.TaskID,tenant:selection.tenant,class:selection.class,cost:cost,createdAt:candidate.CreatedAt}
 d.pending=replacement
 return replacement,nil
}
