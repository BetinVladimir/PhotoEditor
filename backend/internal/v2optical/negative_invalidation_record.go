package v2optical

import (
 "context"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

// writeNegativeInvalidation recomputes the inventory before passing a plan to
// the SQL writer, which repeats completeness under locks. A successful record
// denies new publication/consumers; it does not settle cancellation or replan.
func writeNegativeInvalidation(ctx context.Context,db NegativeTransactions,who auth.Identity,tenant,project,review,originTask,originRun uuid.UUID,decisionHash,route string,maxTasks,maxEdges int, cancelConsumers bool)(NegativeInvalidationPlan,error) {
 return executeNegativeInvalidation(ctx,db,who,tenant,project,review,originTask,originRun,decisionHash,route,maxTasks,maxEdges,cancelConsumers,nil,false)
}

func executeNegativeInvalidation(ctx context.Context,db NegativeTransactions,who auth.Identity,tenant,project,review,originTask,originRun uuid.UUID,decisionHash,route string,maxTasks,maxEdges int,cancelConsumers bool,settlement *NegativeInvalidationSettlement,service bool)(NegativeInvalidationPlan,error) {
 var zero NegativeInvalidationPlan
 if err:=ctx.Err();err!=nil { return zero,err }
 if review==uuid.Nil||maxTasks<1||maxTasks>4096||maxEdges<0||maxEdges>65536 { return zero,v2input.ErrInvalid }
 if _,err:=domain.ParseDigest(decisionHash);err!=nil { return zero,v2input.ErrInvalid }
 plan,err:=readNegativeDependencyPlan(ctx,db,who,tenant,project,originTask,originRun,route,maxTasks,maxEdges,service);if err!=nil { return zero,err }
 raw,err:=canonical.Marshal(plan);if err!=nil { return zero,err }
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=db.Begin(ctx);if err!=nil { return zero,err };if tx==nil { return zero,v2input.ErrInvalid }
 defer tx.Rollback(context.WithoutCancel(ctx))
 var role string
 if err:=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil { return zero,err };if role!="photo_scheduler" { return zero,v2input.ErrInvalid }
 if _,err:=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tenant.String());err!=nil { return zero,err }
 var actor,session any=who.UserID,who.SessionID
 if service {actor=nil;session=nil}
 var recorded bool
 query:="SELECT photo.record_v29_negative_invalidation($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9)"
 if cancelConsumers { query="SELECT photo.request_v29_negative_invalidation_cancel($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9)" }
 var observed NegativeInvalidationSettlement
 if settlement!=nil {
  query="SELECT photo.reconcile_v29_negative_invalidation($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9)"
  var body []byte
  if err:=tx.QueryRow(ctx,query,tenant,project,actor,session,review,decisionHash,string(raw),maxTasks,maxEdges).Scan(&body);err!=nil { return zero,err }
  var err error
  observed,err=decodeNegativeInvalidationSettlement(body,plan);if err!=nil { return zero,err }
 } else {
  if err:=tx.QueryRow(ctx,query,tenant,project,actor,session,review,decisionHash,string(raw),maxTasks,maxEdges).Scan(&recorded);err!=nil { return zero,err }
  if !recorded { return zero,v2input.ErrExpired }
 }
 if err:=tx.Commit(ctx);err!=nil { return zero,err }
 if settlement!=nil { *settlement=observed }
 return plan,nil
}

// RecordNegativeInvalidation records publication/consumer invalidation only.
// Use RequestNegativeInvalidationCancel to also request cooperative cancellation.
func RecordNegativeInvalidation(ctx context.Context,db NegativeTransactions,who auth.Identity,tenant,project,review,originTask,originRun uuid.UUID,decisionHash,route string,maxTasks,maxEdges int)(NegativeInvalidationPlan,error) {
 return writeNegativeInvalidation(ctx,db,who,tenant,project,review,originTask,originRun,decisionHash,route,maxTasks,maxEdges,false)
}

// RequestNegativeInvalidationCancel atomically revalidates/records the complete
// ledger and requests cooperative cancellation. Success is not lease settlement
// or a replacement-graph acknowledgement; callers must observe those separately.
func RequestNegativeInvalidationCancel(ctx context.Context,db NegativeTransactions,who auth.Identity,tenant,project,review,originTask,originRun uuid.UUID,decisionHash,route string,maxTasks,maxEdges int)(NegativeInvalidationPlan,error) {
 return writeNegativeInvalidation(ctx,db,who,tenant,project,review,originTask,originRun,decisionHash,route,maxTasks,maxEdges,true)
}
