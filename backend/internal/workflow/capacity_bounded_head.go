package workflow

import (
 "context"
 "errors"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

// CapacityBoundedHeadChoice contains one selected FIFO head and the full
// runnable-cost cap. A nil head means no task currently fits the deficits.
type CapacityBoundedHeadChoice struct {
 Head *CapacityCandidate
 EffectiveClass string
 MaxRunnableCostSeconds int64
}

// ReadCapacityBoundedHeadChoice transfers only one head from SQL. SQL considers
// every tenant/class before choosing; excluded heads are local to this cycle.
func ReadCapacityBoundedHeadChoice(ctx context.Context,tx pgx.Tx,pool string,now time.Time,
 policy CapacityQueuePolicy,lastTenant uuid.UUID,classCursor int,
 deficits map[string]int64,excluded []uuid.UUID)(CapacityBoundedHeadChoice,error){
 empty:=CapacityBoundedHeadChoice{}
 if err:=ctx.Err();err!=nil{return empty,err}
 if tx==nil||now.IsZero()||(pool!="cpu"&&pool!="gpu"&&pool!="signer")||policy.Validate()!=nil||classCursor<0||classCursor>=len(capacityClasses)||len(excluded)>100{
  return empty,errors.New("invalid bounded capacity head source")
 }
 ordered:=make([]int64,len(capacityClasses))
 for i,class:=range capacityClasses{
  value,ok:=deficits[class];if !ok||value<0||value>capacitySafeInteger{return empty,errors.New("invalid bounded capacity deficits")}
  ordered[i]=value
 }
 if len(deficits)!=len(capacityClasses){return empty,errors.New("invalid bounded capacity deficits")}
 ownedExcluded:=make([]uuid.UUID,len(excluded));copy(ownedExcluded,excluded)
 seen:=make(map[uuid.UUID]bool,len(excluded))
 for _,id:=range ownedExcluded{if id==uuid.Nil||seen[id]{return empty,errors.New("invalid bounded capacity exclusion")};seen[id]=true}
 var taskID,tenantID *uuid.UUID
 var originalQueue,effectiveQueue *string
 var createdAt *time.Time
 var estimatedMs,tenantQueued *int64
 var heavy *bool
 var capSeconds int64
 err:=tx.QueryRow(ctx,`SELECT task_id,tenant_id,original_queue,created_at,estimated_ms,heavy,tenant_queued_heavy,effective_queue,max_runnable_cost_seconds
FROM photo.capacity_queue_bounded_head_choice($1,$2,$3,$4,$5,$6,$7,$8)`,
  pool,now,policy.UnknownCostSeconds,policy.TenantHeavyQueued,lastTenant,classCursor,ordered,ownedExcluded).
  Scan(&taskID,&tenantID,&originalQueue,&createdAt,&estimatedMs,&heavy,&tenantQueued,&effectiveQueue,&capSeconds)
 if err!=nil{return empty,err}
 if err=ctx.Err();err!=nil{return empty,err}
 if capSeconds<3600||capSeconds>capacitySafeInteger{return empty,errors.New("invalid bounded capacity cap")}
 result:=CapacityBoundedHeadChoice{MaxRunnableCostSeconds:capSeconds}
 if taskID==nil{
  if tenantID!=nil||originalQueue!=nil||createdAt!=nil||estimatedMs!=nil||heavy!=nil||tenantQueued!=nil||effectiveQueue!=nil{return empty,errors.New("partial bounded capacity head")}
  return result,nil
 }
 if tenantID==nil||originalQueue==nil||createdAt==nil||heavy==nil||tenantQueued==nil||effectiveQueue==nil||seen[*taskID]{return empty,errors.New("invalid bounded capacity head")}
 candidate:=CapacityCandidate{TaskID:*taskID,TenantID:*tenantID,OriginalQueue:*originalQueue,CreatedAt:*createdAt,EstimatedMilliseconds:estimatedMs,Heavy:*heavy,TenantQueuedHeavy:*tenantQueued}
 cost,class,e:=(&CapacityDRR{policy:policy}).describe(candidate,now)
 if e!=nil||class!=*effectiveQueue||candidate.Heavy&&candidate.TenantQueuedHeavy>=policy.TenantHeavyQueued||cost>deficits[class]||cost>capSeconds{return empty,errors.New("invalid bounded capacity selection")}
 result.Head=&candidate;result.EffectiveClass=class
 return result,nil
}
