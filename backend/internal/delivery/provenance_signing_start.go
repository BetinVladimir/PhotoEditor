package delivery

import (
 "context"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

// DeliverySigner is the installed tenant-scoped V31 SignExport service. It
// must enforce the current editor session and the full V31 qualification.
type DeliverySigner interface {
 SignExport(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID,string)(v2input.JobTicket,error)
}

type SigningChildTicket struct {
 Dependency SigningDependency
 Ticket v2input.JobTicket
}

// StartDeliverySigningDependencies invokes the existing V31 admission with
// deterministic per-slot keys. It is for a currently authorized editor call;
// a scheduler must not replay a saved user session as delegated authority.
// Partial success is safely retryable with the same keys. A queued ticket is
// not evidence of signed completion or permission to assemble the package.
func StartDeliverySigningDependencies(ctx context.Context,who auth.Identity,
 tenant,parent uuid.UUID,frozen FrozenInput,slots []Slot,signer DeliverySigner)([]SigningChildTicket,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if who.UserID==uuid.Nil||who.SessionID==uuid.Nil||tenant==uuid.Nil||signer==nil{return nil,v2input.ErrInvalid}
 choice,err:=DeliverySigningChoice(frozen);if err!=nil{return nil,err}
 dependencies,err:=CompileSigningDependencies(parent,choice.PolicyID,slots)
 if err!=nil{return nil,err}
 out:=make([]SigningChildTicket,0,len(dependencies))
 for _,dependency:=range dependencies{
  if err:=ctx.Err();err!=nil{return nil,err}
  ticket,err:=signer.SignExport(ctx,who,tenant,dependency.ExportID,choice.PolicyID,dependency.IdempotencyKey)
  if err!=nil{return nil,err}
  workflow:=uuid.NewSHA1(tenant,[]byte("provenance:"+who.UserID.String()+":"+dependency.IdempotencyKey))
  if ticket.WorkflowID!=workflow||ticket.RunID!=uuid.NewSHA1(workflow,[]byte("run"))||ticket.State!="queued"{
   return nil,v2input.ErrInvalid
  }
  out=append(out,SigningChildTicket{Dependency:dependency,Ticket:ticket})
 }
 return out,nil
}
