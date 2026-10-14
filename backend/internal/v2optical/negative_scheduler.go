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

type NegativeSchedulerDecision struct {
 Tenant uuid.UUID `json:"tenant_id"`
 Project uuid.UUID `json:"project_id"`
 Decision NegativeDeclinedDecision `json:"decision"`
}
type NegativeSchedulerBackend interface {
 Next(context.Context,uuid.UUID)(*NegativeSchedulerDecision,error)
 Reconcile(context.Context,NegativeSchedulerDecision)error
}

// NegativeScheduler processes one persisted decision per independent loop call.
// A failed candidate advances this sweep's cursor so it cannot starve unrelated
// tenants; the next sweep retries it. This object has one loop owner.
type NegativeScheduler struct { Backend NegativeSchedulerBackend; after uuid.UUID }
func(s *NegativeScheduler)ProcessOne(ctx context.Context)(bool,error) {
 if err:=ctx.Err();err!=nil {return false,err}
 if s==nil||s.Backend==nil {return false,v2input.ErrInvalid}
 d,err:=s.Backend.Next(ctx,s.after);if err!=nil {return false,err}
 if d==nil {s.after=uuid.Nil;return false,nil}
 if d.Tenant==uuid.Nil||d.Project==uuid.Nil||bytes.Compare(d.Decision.Review[:],s.after[:])<=0 {return false,v2input.ErrInvalid}
 // Advance only in memory. Restart or the next sweep rediscovers every review.
 s.after=d.Decision.Review
 if err:=s.Backend.Reconcile(ctx,*d);err!=nil {return false,err}
 return true,nil
}

type SQLNegativeScheduler struct { DB NegativeTransactions }
func decodeNegativeSchedulerDecision(raw []byte,after uuid.UUID)(*NegativeSchedulerDecision,error) {
 if len(raw)<2||len(raw)>65536 {return nil,v2input.ErrInvalid}
 normal,err:=canonical.Bytes(raw);if err!=nil {return nil,v2input.ErrInvalid}
 var page []NegativeSchedulerDecision
 if json.Unmarshal(normal,&page)!=nil||page==nil||len(page)>1 {return nil,v2input.ErrInvalid}
 exact,err:=canonical.Marshal(page);if err!=nil||!bytes.Equal(exact,normal) {return nil,v2input.ErrInvalid}
 if len(page)==0 {return nil,nil}
 d:=page[0];if d.Tenant==uuid.Nil||d.Project==uuid.Nil {return nil,v2input.ErrInvalid}
 body,err:=canonical.Marshal([]NegativeDeclinedDecision{d.Decision});if err!=nil {return nil,err}
 if _,err=decodeNegativeDeclinedDecisions(body,after,1);err!=nil {return nil,err}
 return &d,nil
}
func(s SQLNegativeScheduler)Next(ctx context.Context,after uuid.UUID)(*NegativeSchedulerDecision,error) {
 if err:=ctx.Err();err!=nil {return nil,err};if s.DB==nil {return nil,v2input.ErrInvalid}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=s.DB.Begin(ctx);if err!=nil {return nil,err};if tx==nil {return nil,v2input.ErrInvalid};defer tx.Rollback(context.WithoutCancel(ctx))
 var role string
 if err:=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil {return nil,err};if role!="photo_scheduler" {return nil,v2input.ErrInvalid}
 var cursor any;if after!=uuid.Nil {cursor=after}
 var raw []byte
 if err:=tx.QueryRow(ctx,"SELECT photo.read_v29_negative_scheduler_decisions($1,1)",cursor).Scan(&raw);err!=nil {return nil,err}
 d,err:=decodeNegativeSchedulerDecision(raw,after);if err!=nil {return nil,err}
 if err:=tx.Commit(ctx);err!=nil {return nil,err};return d,nil
}
func(s SQLNegativeScheduler)Reconcile(ctx context.Context,d NegativeSchedulerDecision)error {
 var status NegativeInvalidationSettlement
 plan,err:=executeNegativeInvalidation(ctx,s.DB,auth.Identity{},d.Tenant,d.Project,d.Decision.Review,d.Decision.OriginTask,d.Decision.OriginRun,d.Decision.DecisionHash,d.Decision.Route,4096,65536,true,&status,true)
 if err!=nil {return err}
 if d.Decision.Route=="skip"&&status.Settled {return closeNegativeSkippedDependencies(ctx,s.DB,auth.Identity{},d.Tenant,d.Project,d.Decision.Review,d.Decision.DecisionHash,plan,true)}
 return nil
}
