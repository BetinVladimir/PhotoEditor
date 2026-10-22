package interchange
import(
 "context"
 "encoding/json"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/tenant"
)
func(s Service)CloseImportReview(ctx context.Context,who auth.Identity,tid,workflow uuid.UUID,key string,raw []byte,bucket string,objects artifactjson.Objects)(idempotency.Response,error){
 intent,err:=FreezeImportReviewIntent(ctx,tid,workflow,who,key,"close",raw);if err!=nil{return idempotency.Response{},err}
 recover:=func()(idempotency.Response,bool,error){
  var response idempotency.Response;var complete bool
  err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
   var outcome string;var body []byte
   if err:=tx.QueryRow(ctx,`SELECT outcome,response_body FROM photo.v30_interchange_close_replay($1,$2,$3,$4,$5,$6)`,tid,workflow,who.UserID,who.SessionID,key,intent.requestSHA).Scan(&outcome,&body);err!=nil{return err}
   switch outcome{case "absent","pending":return nil;case "conflict":return domain.NewError("IDEMPOTENCY_CONFLICT",409);case "replayed":complete=true;id:=workflow;response=idempotency.Response{Status:200,Body:append(json.RawMessage(nil),body...),ResourceID:&id,Replayed:true};return nil;default:return domain.NewError("INTERCHANGE_CLOSE_INVALID_RECEIPT",503)}
  });return response,complete,err
 }
 if response,complete,err:=recover();err!=nil||complete{return response,err}
 if !reviewDocumentInstalled(bucket,objects){return idempotency.Response{},domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
 var run uuid.UUID
 err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var contextRaw []byte
  if err:=tx.QueryRow(ctx,`SELECT photo.v30_interchange_review_context($1,$2,$3,$4,$5,$6)`,tid,who.UserID,who.SessionID,workflow,intent.revision,(SQLReviewDocumentRepository{Intent:intent}).reportSHA()).Scan(&contextRaw);err!=nil{return err}
  var current struct{Run uuid.UUID `json:"run_id"`};if json.Unmarshal(contextRaw,&current)!=nil||current.Run==uuid.Nil{return domain.NewError("INTERCHANGE_REVIEW_STALE",409)};run=current.Run;return nil
 })
 if err!=nil{if response,complete,recoveryErr:=recover();recoveryErr!=nil||complete{return response,recoveryErr};return idempotency.Response{},err}
 repository:=SQLReviewDocumentRepository{DB:s.DB,Intent:intent,Run:run,Bucket:bucket}
 selection,publishErr:=(artifactjson.Publisher{Repository:repository,Objects:objects}).Publish(ctx,repository.Scope(),intent.DocumentBytes())
 if response,complete,err:=recover();err!=nil||complete{return response,err}
 if publishErr!=nil{return idempotency.Response{},publishErr}
 var response idempotency.Response
 err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var authorized bool
  if err:=tx.QueryRow(ctx,`SELECT photo.v2_editor_authorized($1,(SELECT project_id FROM photo.v2_workflows WHERE tenant_id=$1 AND id=$2),$3,$4) IS TRUE`,tid,workflow,who.UserID,who.SessionID).Scan(&authorized);err!=nil{return err}
  if !authorized{return domain.NewError("FORBIDDEN",403)}
  var request any;if err:=json.Unmarshal(intent.RequestBytes(),&request);err!=nil{return err}
  var err error
  response,err=idempotency.Use(ctx,tx,idempotency.Scope{Tenant:tid,Principal:who.UserID,PrincipalType:"user",Route:"Interchange.CloseReview",Key:key},request,time.Now().Add(24*time.Hour),func()(idempotency.Response,error){
   body,err:=intent.CommitClose(ctx,tx,selection);if err!=nil{return idempotency.Response{},err};id:=workflow;return idempotency.Response{Status:200,Body:body,ResourceID:&id},nil
  })
  if err!=nil{return err}
  authorized=false
  if err=tx.QueryRow(ctx,`SELECT photo.v2_editor_authorized($1,(SELECT project_id FROM photo.v2_workflows WHERE tenant_id=$1 AND id=$2),$3,$4) IS TRUE`,tid,workflow,who.UserID,who.SessionID).Scan(&authorized);err!=nil{return err}
  if !authorized{return domain.NewError("FORBIDDEN",403)}
  return nil
 });return response,err
}
