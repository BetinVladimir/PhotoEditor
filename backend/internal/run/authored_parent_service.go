package run

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/project"
 "photoeditor/backend/internal/tenant"
)

// BeginAuthoredRender is an immutable parent admission, not an output collection
// acceptance. It loads the complete original recipe only for fresh admission;
// accepted retries authenticate the retained request before provider/object IO.
func(s EditorEffectsService)BeginAuthoredRender(ctx context.Context,who auth.Identity,tid,pid uuid.UUID,key string,req EditorEffectRequest)(idempotency.Response,error){
 invalid:=func()(idempotency.Response,error){return idempotency.Response{},domain.NewError("VALIDATION_FAILED",422)}
 if s.Runs.DB==nil{return idempotency.Response{},domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
 if tid==uuid.Nil||pid==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||!idempotency.ValidKey(key)||
 req.Source.AssetVersionID==uuid.Nil||req.Source.Source.ArtifactID==uuid.Nil||req.Recipe.ArtifactID==uuid.Nil||
 req.FrozenDocumentID==uuid.Nil||req.DraftDocumentID==uuid.Nil||req.DraftRevision<1||req.DraftRevision>9007199254740991||
 !isLowerHexSHA(req.Source.Source.SHA256)||!isLowerHexSHA(req.Source.CanvasHash)||!isLowerHexSHA(req.Recipe.SHA256){return invalid()}
 parent:=uuid.NewSHA1(tid,[]byte("V26.AuthoredRender:"+who.UserID.String()+":"+pid.String()+":"+key))
 requestSHA,err:=canonical.Hash(req);if err!=nil{return invalid()}
 replay,found,err:=s.replayAuthoredParent(ctx,who,tid,pid,parent,requestSHA)
 if err!=nil||found{return replay,err}
 prepared,err:=s.prepareAuthoredParent(ctx,who,tid,pid,key,req);if err!=nil{return idempotency.Response{},err}
 accepted,err:=s.admitAuthoredParent(ctx,who,tid,pid,prepared);if err!=nil{return idempotency.Response{},err}
 raw,err:=encodeAuthoredParentAccepted(accepted);if err!=nil{return idempotency.Response{},err}
 return idempotency.Response{Status:202,Body:raw,ResourceID:&accepted.RunID,Replayed:accepted.Replayed},nil
}
func(s EditorEffectsService)replayAuthoredParent(ctx context.Context,who auth.Identity,tid,pid,parent uuid.UUID,requestSHA string)(idempotency.Response,bool,error){
 var out idempotency.Response;found:=false
 err:=tenant.WithTenant(ctx,s.Runs.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  if _,err:=project.Authorize(ctx,tx,tid,pid,who.UserID,role,domain.Editor,false);err!=nil{return err}
  var raw []byte
  if err:=tx.QueryRow(ctx,`SELECT photo.authored_render_parent_accepted($1,$2,$3,$4,$5)`,tid,parent,pid,who.UserID,who.SessionID).Scan(&raw);err!=nil{return err}
  if len(raw)==0||string(raw)=="null"{return nil}
  var receipt struct{Parent uuid.UUID `json:"parent_run_id"`;RequestSHA string `json:"request_sha"`}
  if decodeAuthoredReceipt(raw,&receipt)!=nil||receipt.Parent!=parent||!isLowerHexSHA(receipt.RequestSHA){return domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
  if receipt.RequestSHA!=requestSHA{return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
  body,err:=encodeAuthoredParentAccepted(AuthoredParentAccepted{RunID:parent,Status:"running"});if err!=nil{return err}
  out=idempotency.Response{Status:202,Body:body,ResourceID:&parent,Replayed:true};found=true;return nil
 })
 return out,found,err
}
func decodeAuthoredReceipt(raw []byte,target any)error{
 // Reuse the closed decoder for SQL-origin bytes; never accept unknown fields
 // or a second JSON value as an immutable accepted receipt.
 return decodeAuthoredJSON(raw,target)
}
