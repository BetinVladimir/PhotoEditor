package interchange

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
)

// Installed preparation must commit the private draft, publish/register inputs
// outside activation, and return the native/resource-qualified private receipt.
type ExportCreatePreparer interface{
 PrepareExportCreation(context.Context,auth.Identity,uuid.UUID,CreateRequest,string)(PreparedExportActivation,error)
}
type exportCreatePhases struct{
 recover func(context.Context)(idempotency.Response,bool,error)
 prepare func(context.Context)(PreparedExportActivation,error)
 activate func(context.Context,PreparedExportActivation)(idempotency.Response,error)
}
func coordinateExportCreate(ctx context.Context,p exportCreatePhases)(idempotency.Response,error){
 var zero idempotency.Response
 if err:=ctx.Err();err!=nil{return zero,err}
 response,found,err:=p.recover(ctx);if err!=nil{return zero,err};if found{return response,nil}
 prepared,prepareErr:=p.prepare(ctx)
 if err:=ctx.Err();err!=nil{return zero,err}
 response,found,err=p.recover(ctx);if err!=nil{return zero,err};if found{return response,nil}
 if prepareErr!=nil{return zero,prepareErr}
 response,activationErr:=p.activate(ctx,prepared)
 if err:=ctx.Err();err!=nil{return zero,err}
 if activationErr==nil{return response,nil}
 // A concurrent caller or an uncertain commit may already have saved a ticket.
 // Only current authorized recovery can turn this failure into a replay.
 response,found,err=p.recover(ctx);if err!=nil{return zero,err};if found{return response,nil}
 return zero,activationErr
}
func(s Service)CreateExport(ctx context.Context,who auth.Identity,tid uuid.UUID,request CreateRequest,key string,preparer ExportCreatePreparer)(idempotency.Response,error){
 intent,err:=FreezeCreateIntent(tid,who,request,key);if err!=nil{return idempotency.Response{},err}
 if intent.Request.Direction!="export"{return idempotency.Response{},domain.NewError("VALIDATION_FAILED",422)}
 return coordinateExportCreate(ctx,exportCreatePhases{
  recover:func(ctx context.Context)(idempotency.Response,bool,error){return s.RecoverExportTicket(ctx,who,tid,copyExportIntent(intent).Request,key)},
  prepare:func(ctx context.Context)(PreparedExportActivation,error){
   if preparer==nil{return PreparedExportActivation{},domain.NewError("INTERCHANGE_FORMAT_UNAVAILABLE",503)}
   return preparer.PrepareExportCreation(ctx,who,tid,copyExportIntent(intent).Request,key)
  },
  activate:func(ctx context.Context,prepared PreparedExportActivation)(idempotency.Response,error){return s.ActivateExport(ctx,who,tid,copyExportIntent(intent).Request,key,prepared)},
 })
}
