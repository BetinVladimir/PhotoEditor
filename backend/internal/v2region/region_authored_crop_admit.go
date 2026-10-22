package v2region

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/v2input"
)

// Admit uses the normal V2 blocked shell, real transport publication and graph
// seal. Both SQL phases repeat the compound request's current authority. No
// transaction or row lock is held while transport bytes are written. A failed
// publication or seal leaves the same blocked child recoverable by its key.
func(n FrozenAuthoredCropNode)Admit(ctx context.Context,db *pgxpool.Pool,publisher v2input.Publisher,snapshot uuid.UUID)(v2input.JobTicket,error){
 var empty v2input.JobTicket
 if db==nil||publisher==nil{return empty,ErrRegionUnavailable}
 if err:=ctx.Err();err!=nil{return empty,err}
 var role string
 if err:=db.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil{return empty,err}
 if role!="photo_scheduler"{return empty,ErrRegionInvalid}
 shell,sources,key,err:=n.DraftShell(snapshot);if err!=nil{return empty,err}
 var outcome string;var run uuid.UUID
 err=database.TenantTransaction(ctx,db,shell.Tenant,func(tx pgx.Tx)error{
  var e error;outcome,run,e=n.BeginInTransaction(ctx,tx,snapshot);return e
 });if err!=nil{return empty,err}
 switch outcome{
 case "conflict":return empty,v2input.ErrConflict
 case "expired":return empty,v2input.ErrExpired
 case "created","pending","replayed":if run!=shell.Run{return empty,ErrRegionInvalid}
 default:return empty,ErrRegionInvalid
 }
 if outcome!="replayed"{
  cursor,e:=sources[0].Open(ctx);if e!=nil{return empty,e};if cursor==nil{return empty,ErrRegionInvalid}
  built,buildErr:=v2input.BuildInputClosure(ctx,sources[0].ID,cursor,publisher)
  if e=finishRegionInputCursor(cursor,buildErr);e!=nil{return empty,e}
  if e=ctx.Err();e!=nil{return empty,e}
  if e=database.TenantTransaction(ctx,db,shell.Tenant,func(tx pgx.Tx)error{return n.SealInTransaction(ctx,tx,snapshot,built)});e!=nil{return empty,e}
 }
 ticket,err:=(v2input.SQLAdmissionStore{DB:db}).Ticket(ctx,shell.Tenant,n.plan.intent.Actor,key,n.plan.sha)
 if err!=nil{return empty,err}
 if ticket.RunID!=shell.Run||ticket.WorkflowID!=shell.WorkflowID||ticket.State!="queued"{return empty,ErrRegionInvalid}
 // A replay ticket acknowledges graph admission only. Canonical completion,
 // independent adoption and live-draft authority are inspected separately.
 return ticket,nil
}
