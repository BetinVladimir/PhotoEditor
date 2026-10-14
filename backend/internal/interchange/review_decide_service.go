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
 "photoeditor/backend/internal/v2draft"
)
func(s Service) importApprovalInstalled(bucket string,objects artifactjson.Objects,drafts v2draft.Service)bool{
 return s.DB!=nil&&drafts.DB==s.DB&&drafts.Objects!=nil&&bucket!=""&&drafts.Bucket==bucket&&objects!=nil
}
func reviewDocumentInstalled(bucket string,objects artifactjson.Objects)bool{return bucket!=""&&objects!=nil}
func(s Service)DecideImport(ctx context.Context,who auth.Identity,tid,workflow uuid.UUID,key string,raw []byte,bucket string,objects artifactjson.Objects,drafts v2draft.Service)(idempotency.Response,error){
 intent,err:=FreezeImportReviewIntent(ctx,tid,workflow,who,key,"decide",raw);if err!=nil{return idempotency.Response{},err}
 recover:=func()(idempotency.Response,bool,error){
  var response idempotency.Response;var complete bool
  err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
   var outcome string;var body []byte
   if err:=tx.QueryRow(ctx,`SELECT outcome,response_body FROM photo.v30_interchange_decide_replay($1,$2,$3,$4,$5,$6)`,tid,workflow,who.UserID,who.SessionID,key,intent.requestSHA).Scan(&outcome,&body);err!=nil{return err}
   switch outcome{case "absent","pending":return nil;case "conflict":return domain.NewError("IDEMPOTENCY_CONFLICT",409);case "replayed":complete=true;id:=intent.owner;response=idempotency.Response{Status:200,Body:append(json.RawMessage(nil),body...),ResourceID:&id,Replayed:true};return nil;default:return domain.NewError("INTERCHANGE_DECISION_INVALID_RECEIPT",503)}
  });return response,complete,err
 }
 if response,complete,err:=recover();err!=nil||complete{return response,err}
 if !reviewDocumentInstalled(bucket,objects){return idempotency.Response{},domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
 var batch PreparedImportDecisionBatch
 err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{var err error;batch,err=intent.SelectDecisionBatch(ctx,tx);return err})
 if err!=nil{if response,complete,recoveryErr:=recover();recoveryErr!=nil||complete{return response,recoveryErr};return idempotency.Response{},err}
 var selected []SelectedImportDecision
 if err:=json.Unmarshal(batch.Bytes(),&selected);err!=nil{return idempotency.Response{},err}
 resolved:=make([]ResolvedImportApproval,0,batch.ApprovalCount())
 var parsed []byte
 var reviewRequest ImportReviewRequest
 if err:=json.Unmarshal(raw,&reviewRequest);err!=nil{return idempotency.Response{},err}

 for _,decision:=range selected{
  if err:=ctx.Err();err!=nil{return idempotency.Response{},err}
  if decision.Decision!="approve"{continue}
  if !s.importApprovalInstalled(bucket,objects,drafts){return idempotency.Response{},domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
  if parsed==nil{
   source:=SQLImportReviewParsedSource{DB:s.DB,Tenant:tid,Workflow:workflow,Who:who,Revision:intent.ExpectedRevision(),ReportSHA:reviewRequest.ReportSHA,Bucket:drafts.Bucket}
   var readErr error;parsed,readErr=ReadImportReviewParsed(ctx,source,drafts.Objects)
   if readErr!=nil{if response,complete,recoveryErr:=recover();recoveryErr!=nil||complete{return response,recoveryErr};return idempotency.Response{},readErr}
  }
  prepared,prepareErr:=PrepareImportApprovalFromReview(ctx,drafts,who,tid,batch.ProjectID(),workflow,batch.RunID(),decision.Target,intent.ExpectedRevision(),reviewRequest.ReportSHA,parsed,s.ApprovalStyles,s.ApprovalStyleFactory)
  if prepareErr!=nil{if response,complete,recoveryErr:=recover();recoveryErr!=nil||complete{return response,recoveryErr};return idempotency.Response{},prepareErr}
  resolved=append(resolved,prepared)
 }
 repository:=SQLReviewDocumentRepository{DB:s.DB,Intent:intent,Run:batch.RunID(),Bucket:bucket}
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
  response,err=idempotency.Use(ctx,tx,idempotency.Scope{Tenant:tid,Principal:who.UserID,PrincipalType:"user",Route:"Interchange.DecideImport",Key:key},request,time.Now().Add(24*time.Hour),func()(idempotency.Response,error){
   body,err:=intent.CommitDecisions(ctx,tx,selection,batch,resolved);if err!=nil{return idempotency.Response{},err};id:=intent.owner;return idempotency.Response{Status:200,Body:body,ResourceID:&id},nil
  });return err
 });return response,err
}
