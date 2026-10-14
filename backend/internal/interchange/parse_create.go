package interchange
import(
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/tenant"
)
type parseCreatePhases struct{
 recover func(context.Context)(ParseRecovery,error)
 prepare func(context.Context)error
 activate func(context.Context,ParsePlan)(idempotency.Response,error)
}
func coordinateParseCreate(ctx context.Context,p parseCreatePhases)(idempotency.Response,error){
 if err:=ctx.Err();err!=nil{return idempotency.Response{},err}
 current,err:=p.recover(ctx);if err!=nil{return idempotency.Response{},err}
 if current.Completed{return current.Response,nil}
 preparationErr:=p.prepare(ctx)
 if err:=ctx.Err();err!=nil{return idempotency.Response{},err}
 // A concurrent caller may complete while any preparation stage is running.
 // Current authorized recovery alone can convert that race into ticket replay.
 current,err=p.recover(ctx);if err!=nil{return idempotency.Response{},err}
 if current.Completed{return current.Response,nil}
 if preparationErr!=nil{return idempotency.Response{},preparationErr}
 if !current.HasPending{return idempotency.Response{},domain.NewError("INTERCHANGE_CREATE_STALE",409)}
 response,activationErr:=p.activate(ctx,current.Plan)
 if err:=ctx.Err();err!=nil{return idempotency.Response{},err}
 if activationErr==nil{return response,nil}
 // A commit may succeed even when its acknowledgement is lost. Only the
 // current authorized read may turn that outcome into a replayed ticket.
 current,err=p.recover(ctx);if err!=nil{return idempotency.Response{},err}
 if current.Completed{return current.Response,nil}
 return idempotency.Response{},activationErr
}
func(s Service)CreateParse(ctx context.Context,who auth.Identity,tid uuid.UUID,request CreateRequest,key string,deps ParseDependencies)(idempotency.Response,error){
 intent,err:=FreezeCreateIntent(tid,who,request,key);if err!=nil{return idempotency.Response{},err};request=intent.Request
 return coordinateParseCreate(ctx,parseCreatePhases{
  recover:func(ctx context.Context)(ParseRecovery,error){return s.RecoverParse(ctx,who,tid,request,key)},
  prepare:func(ctx context.Context)error{_,err:=s.PrepareParse(ctx,who,tid,request,key,deps);return err},
  activate:func(ctx context.Context,plan ParsePlan)(idempotency.Response,error){
   var result idempotency.Response
   err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{var err error;result,err=plan.ActivateParse(ctx,tx,who,tid,request,key);return err})
   return result,err
  },
 })
}
