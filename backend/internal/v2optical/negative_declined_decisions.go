package v2optical

import (
 "bytes"
 "context"
 "encoding/json"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

type NegativeDeclinedDecision struct {
 Review uuid.UUID `json:"review_id"`
 OriginTask uuid.UUID `json:"origin_task_id"`
 OriginRun uuid.UUID `json:"origin_run_id"`
 DecisionHash string `json:"decision_hash"`
 Route string `json:"route"`
}

func decodeNegativeDeclinedDecisions(raw []byte,after uuid.UUID,limit int)([]NegativeDeclinedDecision,error) {
 if limit<1||limit>100||len(raw)<2||len(raw)>65536 {return nil,v2input.ErrInvalid}
 normal,err:=canonical.Bytes(raw);if err!=nil {return nil,v2input.ErrInvalid}
 var out []NegativeDeclinedDecision
 if json.Unmarshal(normal,&out)!=nil||out==nil||len(out)>limit {return nil,v2input.ErrInvalid}
 exact,err:=canonical.Marshal(out);if err!=nil||!bytes.Equal(exact,normal) {return nil,v2input.ErrInvalid}
 previous:=after
 for _,d:=range out {
  if d.Review==uuid.Nil||d.OriginTask==uuid.Nil||d.OriginRun==uuid.Nil||bytes.Compare(d.Review[:],previous[:])<=0||(d.Route!="base"&&d.Route!="skip") {return nil,v2input.ErrInvalid}
  if _,err:=domain.ParseDigest(d.DecisionHash);err!=nil {return nil,v2input.ErrInvalid}
  previous=d.Review
 }
 return out,nil
}

func ReadNegativeDeclinedDecisions(ctx context.Context,db NegativeTransactions,who auth.Identity,tenant,project,after uuid.UUID,limit int)([]NegativeDeclinedDecision,error) {
 if err:=ctx.Err();err!=nil {return nil,err}
 if db==nil||tenant==uuid.Nil||project==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||limit<1||limit>100 {return nil,v2input.ErrInvalid}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=db.Begin(ctx);if err!=nil {return nil,err};if tx==nil {return nil,v2input.ErrInvalid};defer tx.Rollback(context.WithoutCancel(ctx))
 var role string
 if err:=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil {return nil,err};if role!="photo_scheduler" {return nil,v2input.ErrInvalid}
 if _,err:=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",tenant.String());err!=nil {return nil,err}
 var cursor any;if after!=uuid.Nil {cursor=after}
 var raw []byte
 if err:=tx.QueryRow(ctx,"SELECT photo.read_v29_negative_declined_decisions($1,$2,$3,$4,$5,$6)",tenant,project,who.UserID,who.SessionID,cursor,limit).Scan(&raw);err!=nil {return nil,err}
 if len(raw)<2||bytes.Equal(bytes.TrimSpace(raw),[]byte("null")) {return nil,v2input.ErrExpired}
 decisions,err:=decodeNegativeDeclinedDecisions(raw,after,limit);if err!=nil {return nil,err}
 if err:=tx.Commit(ctx);err!=nil {return nil,err};return decisions,nil
}

type NegativeDecisionObservation struct {
 Decision NegativeDeclinedDecision
 Plan NegativeInvalidationPlan
 Settlement NegativeInvalidationSettlement
}
type NegativeDecisionPass struct {
 Observations []NegativeDecisionObservation
 // ResumeAfter advances only for reconciliations whose commits succeeded.
 ResumeAfter uuid.UUID
 PageExhausted bool
}

// ReconcileDeclinedDecisions is a bounded scheduler step under a current editor
// identity. On failure, previously committed observations and their cursor are
// returned with the error. Do not treat PageExhausted as permanent completion:
// each new sweep must restart at nil to discover late commits with smaller UUIDs
// and revisit pending cancellation. No replacement is admitted here.
func(p *NegativeProjects) ReconcileDeclinedDecisions(ctx context.Context,who auth.Identity,tenant,project,after uuid.UUID,limit int)(NegativeDecisionPass,error) {
 out:=NegativeDecisionPass{Observations:[]NegativeDecisionObservation{},ResumeAfter:after}
 if p==nil {return out,v2qualification.ErrUnqualified}
 service:=p.services[[2]uuid.UUID{tenant,project}];if service==nil {return out,v2qualification.ErrUnqualified}
 db:=service.coordinator.DB
 decisions,err:=ReadNegativeDeclinedDecisions(ctx,db,who,tenant,project,after,limit);if err!=nil {return out,err}
 for _,d:=range decisions {
  plan,status,err:=ReconcileNegativeInvalidation(ctx,db,who,tenant,project,d.Review,d.OriginTask,d.OriginRun,d.DecisionHash,d.Route,4096,65536)
  if err!=nil {return out,err}
  out.Observations=append(out.Observations,NegativeDecisionObservation{Decision:d,Plan:plan,Settlement:status});out.ResumeAfter=d.Review
 }
 out.PageExhausted=len(decisions)<limit
 return out,nil
}
