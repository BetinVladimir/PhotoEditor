package workflow

import (
 "context"
 "errors"
 "github.com/jackc/pgx/v5"
)

// ReadCapacityCandidates owns the snapshot values. Until qualified task timing
// has a normative persisted binding, SQL emits NULL and policy unknown cost applies.
// Read the complete statement snapshot: truncation would exclude later tenants.
// pgx streams rows; DRR currently retains the full candidate set in memory.
func ReadCapacityCandidates(ctx context.Context,tx pgx.Tx,pool string)([]CapacityCandidate,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if tx==nil || (pool!="cpu"&&pool!="gpu"&&pool!="signer"){return nil,errors.New("invalid capacity candidate source")}
 rows,err:=tx.Query(ctx,"SELECT task_id,tenant_id,original_queue,created_at,estimated_ms,heavy,tenant_queued_heavy FROM photo.capacity_queue_candidates($1)",pool)
 if err!=nil{return nil,err};defer rows.Close()
 result:=make([]CapacityCandidate,0)
 for rows.Next(){
  var candidate CapacityCandidate
  if err=rows.Scan(&candidate.TaskID,&candidate.TenantID,&candidate.OriginalQueue,&candidate.CreatedAt,&candidate.EstimatedMilliseconds,&candidate.Heavy,&candidate.TenantQueuedHeavy);err!=nil{return nil,err}
  if err=ctx.Err();err!=nil{return nil,err}
  result=append(result,candidate)
 }
 if err=rows.Err();err!=nil{return nil,err}
 if err=ctx.Err();err!=nil{return nil,err}
 return result,nil
}
