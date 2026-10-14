package workflow

import (
 "context"
 "errors"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
)

// CapacityHeadSnapshot is a complete SQL-compressed view of one pool at one
// database clock. Affinity is deliberately absent: it needs a second scoped
// authority read before this can replace the full-candidate dispatcher.
type CapacityHeadSnapshot struct {
 Heads []CapacityCandidate
 MaxRunnableCostSeconds int64
}

func ReadCapacityHeadSnapshot(ctx context.Context, tx pgx.Tx, pool string, now time.Time, policy CapacityQueuePolicy) (CapacityHeadSnapshot,error) {
 empty:=CapacityHeadSnapshot{}
 if err:=ctx.Err();err!=nil{return empty,err}
 if tx==nil||now.IsZero()||(pool!="cpu"&&pool!="gpu"&&pool!="signer")||policy.Validate()!=nil{return empty,errors.New("invalid capacity head source")}
 rows,err:=tx.Query(ctx,`SELECT task_id,tenant_id,original_queue,created_at,estimated_ms,heavy,tenant_queued_heavy,effective_queue,max_runnable_cost_seconds
FROM photo.capacity_queue_head_snapshot($1,$2,$3,$4)`,pool,now,policy.UnknownCostSeconds,policy.TenantHeavyQueued)
 if err!=nil{return empty,err};defer rows.Close()
 result:=CapacityHeadSnapshot{MaxRunnableCostSeconds:3600}
 type key struct{tenant uuid.UUID;class string}
 seen:=make(map[key]bool)
 ids:=make(map[uuid.UUID]bool)
 for rows.Next(){
  var candidate CapacityCandidate
  var class string
  var capSeconds int64
  if err=rows.Scan(&candidate.TaskID,&candidate.TenantID,&candidate.OriginalQueue,&candidate.CreatedAt,&candidate.EstimatedMilliseconds,&candidate.Heavy,&candidate.TenantQueuedHeavy,&class,&capSeconds);err!=nil{return empty,err}
  if err=ctx.Err();err!=nil{return empty,err}
  effective,e:=CapacityEffectiveQueue(candidate.OriginalQueue,candidate.CreatedAt,now);if e!=nil||effective!=class{return empty,errors.New("invalid capacity head class")}
  if candidate.TaskID==uuid.Nil||candidate.TenantID==uuid.Nil||candidate.TenantQueuedHeavy<0||candidate.Heavy&&candidate.TenantQueuedHeavy>=policy.TenantHeavyQueued{return empty,errors.New("invalid capacity head")}
  if _,e=CapacityTaskCharge(candidate.EstimatedMilliseconds,policy.UnknownCostSeconds);e!=nil{return empty,e}
  scope:=key{candidate.TenantID,class}
  if seen[scope]||ids[candidate.TaskID]||capSeconds<3600||capSeconds>capacitySafeInteger||len(result.Heads)>0&&capSeconds!=result.MaxRunnableCostSeconds{return empty,errors.New("invalid capacity head snapshot")}
  seen[scope]=true;ids[candidate.TaskID]=true
  result.MaxRunnableCostSeconds=capSeconds
  result.Heads=append(result.Heads,candidate)
 }
 if err=rows.Err();err!=nil{return empty,err}
 if err=ctx.Err();err!=nil{return empty,err}
 return result,nil
}
