package v2optical

import (
 "bytes"
 "context"
 "encoding/json"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// ReadNegativeDependencyPlan reads a bounded project inventory in one statement.
// This snapshot is advisory: mutation must reload it under retained lifecycle,
// run/task/catalogue locks and protect against concurrent new consumers.
func ReadNegativeDependencyPlan(ctx context.Context,db NegativeTransactions,who auth.Identity,tenant,project,originTask,originRun uuid.UUID,route string,maxTasks,maxEdges int)(NegativeInvalidationPlan,error) {
 return readNegativeDependencyPlan(ctx,db,who,tenant,project,originTask,originRun,route,maxTasks,maxEdges,false)
}
// ReadNegativeDependencyPackage retains the original complete inventory needed
// to independently construct replacement topology. The observation grants no
// mutation authority; replacement locks must revalidate the persisted decision.
func ReadNegativeDependencyPackage(ctx context.Context,db NegativeTransactions,who auth.Identity,tenant,project,originTask,originRun uuid.UUID,route string,maxTasks,maxEdges int)(NegativeInvalidationPlan,[]NegativeDependencyTask,error){
 return readNegativeDependencyPackage(ctx,db,who,tenant,project,originTask,originRun,route,maxTasks,maxEdges,false)
}
func readNegativeDependencyPlan(ctx context.Context,db NegativeTransactions,who auth.Identity,tenant,project,originTask,originRun uuid.UUID,route string,maxTasks,maxEdges int,service bool)(NegativeInvalidationPlan,error){
 plan,_,err:=readNegativeDependencyPackage(ctx,db,who,tenant,project,originTask,originRun,route,maxTasks,maxEdges,service);return plan,err
}
func readNegativeDependencyPackage(ctx context.Context,db NegativeTransactions,who auth.Identity,tenant,project,originTask,originRun uuid.UUID,route string,maxTasks,maxEdges int,service bool)(NegativeInvalidationPlan,[]NegativeDependencyTask,error) {
 var zero NegativeInvalidationPlan
 if err:=ctx.Err();err!=nil { return zero,nil,err }
 if db==nil||tenant==uuid.Nil||project==uuid.Nil||originTask==uuid.Nil||originRun==uuid.Nil||(!service&&(who.UserID==uuid.Nil||who.SessionID==uuid.Nil))||(route!="base"&&route!="skip")||maxTasks<1||maxTasks>100000||maxEdges<0||maxEdges>1000000 { return zero,nil,v2input.ErrInvalid }
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=db.Begin(ctx);if err!=nil { return zero,nil,err };if tx==nil { return zero,nil,v2input.ErrInvalid }
 defer tx.Rollback(context.WithoutCancel(ctx))
 var role string
 if err:=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil { return zero,nil,err };if role!="photo_scheduler" { return zero,nil,v2input.ErrInvalid }
 if _,err:=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tenant.String());err!=nil { return zero,nil,err }
 var actor,session any=who.UserID,who.SessionID
 if service {actor=nil;session=nil}
 var raw []byte
 if err:=tx.QueryRow(ctx,"SELECT photo.read_v29_negative_dependency_inventory($1,$2,$3,$4,$5,$6,$7)",tenant,project,actor,session,originTask,maxTasks,maxEdges).Scan(&raw);err!=nil { return zero,nil,err }
 if len(raw)<2 { return zero,nil,v2input.ErrExpired };if len(raw)>64<<20 { return zero,nil,v2input.ErrInvalid }
 normal,err:=canonical.Bytes(raw);if err!=nil { return zero,nil,v2input.ErrInvalid }
 var inventory []NegativeDependencyTask
 if json.Unmarshal(normal,&inventory)!=nil { return zero,nil,v2input.ErrInvalid }
 exact,err:=canonical.Marshal(inventory);if err!=nil||!bytes.Equal(exact,normal) { return zero,nil,v2input.ErrInvalid }
 plan,err:=PlanNegativeInvalidation(ctx,originTask,originRun,route,inventory,maxTasks,maxEdges);if err!=nil { return zero,nil,err }
 if err:=tx.Commit(ctx);err!=nil { return zero,nil,err }
 return plan,inventory,nil
}
