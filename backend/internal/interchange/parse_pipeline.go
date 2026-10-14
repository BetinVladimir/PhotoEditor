package interchange
import(
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)
type ParseDependencies struct{
 Scheduler *pgxpool.Pool
 Bucket string
 Objects artifactjson.Objects
 Documents v2input.DocumentPublisher
 Limits ParseLimits
}
type PreparedParse struct{Identity CreateIdentity;Transport ParseTransport}
// PrepareParse commits resumable draft stages, publishes bytes outside SQL
// transactions, then registers inputs on scheduler and saves API authorization.
// It deliberately returns no public ticket: final seal/activation is separate.
func(s Service)PrepareParse(ctx context.Context,who auth.Identity,tid uuid.UUID,request CreateRequest,key string,deps ParseDependencies)(PreparedParse,error){
 if err:=ctx.Err();err!=nil{return PreparedParse{},err}
 intent,err:=FreezeCreateIntent(tid,who,request,key);if err!=nil{return PreparedParse{},err}
 if s.DB==nil||deps.Scheduler==nil||s.DB==deps.Scheduler||deps.Objects==nil||deps.Documents==nil||len(deps.Bucket)<3||len(deps.Bucket)>63{return PreparedParse{},domain.NewError("INTERCHANGE_UNAVAILABLE",503)}
 request=intent.Request
 plan,err:=s.PinParsePending(ctx,who,tid,request,key,deps.Limits);if err!=nil{return PreparedParse{},err}
 api:=func(fn func(pgx.Tx)error)error{return tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{return fn(tx)})}
 if err:=api(func(tx pgx.Tx)error{return plan.BeginParseRun(ctx,tx,who,tid,request,key)});err!=nil{return PreparedParse{},err}
 publisher:=CreateStoragePublisher{Tenant:tid,Store:SQLCreateDocumentStore{DB:s.DB,Tenant:tid,Key:key,Intent:intent,Who:who,Bucket:deps.Bucket},Objects:deps.Objects}
 if err:=plan.PublishDocuments(ctx,publisher);err!=nil{return PreparedParse{},err}
 if err:=api(func(tx pgx.Tx)error{
  if err:=plan.AttachSnapshot(ctx,tx,who,tid,request,key);err!=nil{return err}
  return plan.MaterializeParseTask(ctx,tx,who,tid,request,key)
 });err!=nil{return PreparedParse{},err}
 transport,err:=plan.PublishInputs(ctx,v2input.ScopedPublisher{Documents:deps.Documents,Tenant:tid,Run:plan.identity.Run});if err!=nil{return PreparedParse{},err}
 if err:=database.TenantTransaction(ctx,deps.Scheduler,tid,func(tx pgx.Tx)error{return transport.RegisterInputs(ctx,tx,tid,plan.identity.Run)});err!=nil{return PreparedParse{},err}
 if err:=api(func(tx pgx.Tx)error{return SaveParseProof(ctx,tx,who,ParseProofScope{Tenant:tid,Run:plan.identity.Run,Task:plan.identity.Task,Params:plan.paramsRef},plan.params)});err!=nil{return PreparedParse{},err}
 return PreparedParse{Identity:plan.identity,Transport:transport},nil
}
