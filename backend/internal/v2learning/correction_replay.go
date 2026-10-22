package v2learning

import (
 "context"
 "errors"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/project"
 "photoeditor/backend/internal/tenant"
)

// A committed correction is recoverable before reopening its expiring source
// documents. A miss grants no admission: RecordCorrection still owns Use.
func(s CorrectionService)readCorrectionReplay(ctx context.Context,who auth.Identity,tid uuid.UUID,key string,req CorrectionCreate)(idempotency.Response,bool,error){
 var out idempotency.Response
 found:=false
 hash,err:=canonical.Hash(req)
 if err!=nil{return out,false,err}
 err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  var pid uuid.UUID
  err:=tx.QueryRow(ctx,`SELECT project_id FROM photo.shoot_sessions WHERE tenant_id=$1 AND id=$2`,tid,req.SessionID).Scan(&pid)
  if errors.Is(err,pgx.ErrNoRows){return domain.NewError("NOT_FOUND",404)}
  if err!=nil{return err}
  if _,err=project.Authorize(ctx,tx,tid,pid,who.UserID,role,domain.Editor,false);err!=nil{return err}
  var storedHash string
  var status *int
  var body []byte
  err=tx.QueryRow(ctx,`SELECT request_hash,response_status,response_body,resource_id FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route='V23.RecordCorrection' AND key=$3`,tid,who.UserID,key).Scan(&storedHash,&status,&body,&out.ResourceID)
  if errors.Is(err,pgx.ErrNoRows){return nil}
  if err!=nil{return err}
  if storedHash!=hash{return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
  if status==nil{return nil}
  if *status!=201 || out.ResourceID==nil{return domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
  var bound bool
  err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM photo.correction_events WHERE tenant_id=$1 AND id=$2 AND project_id=$3 AND asset_version_id=$4)`,tid,*out.ResourceID,pid,req.AssetVersionID).Scan(&bound)
  if err!=nil{return err}
  if !bound{return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
  out.Body,err=canonical.Bytes(body)
  if err!=nil{return err}
  out.Status,out.Replayed,found=*status,true,true
  return ctx.Err()
 })
 if err!=nil{return idempotency.Response{},false,err}
 return out,found,nil
}
