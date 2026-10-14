package interchange

import (
 "bytes"
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

type ArchiveUploadReservation struct{Token uuid.UUID;State,MultipartID string}
type SQLArchiveUploadRepository struct{DB *pgxpool.Pool;Bucket string}
func(s SQLArchiveUploadRepository)validate(ctx context.Context,who auth.Identity,tid uuid.UUID,intent ArchiveUploadIntent)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.DB==nil||len(s.Bucket)<3||len(s.Bucket)>63{return v2input.ErrInvalid}
 p,err:=FreezeArchiveUpload(ctx,tid,who,intent.key,intent.Request());if err!=nil{return err}
 if p.ID()!=intent.ID()||p.ArtifactID()!=intent.ArtifactID()||p.ObjectKey()!=intent.ObjectKey()||p.RequestSHA()!=intent.RequestSHA()||!bytes.Equal(p.RequestBody(),intent.RequestBody()){return v2input.ErrInvalid}
 return nil
}
func validateArchiveReservation(outcome string,id,token *uuid.UUID,state,multipart *string,intent ArchiveUploadIntent)(ArchiveUploadReservation,error){
 var zero ArchiveUploadReservation
 switch outcome{case "quota":return zero,artifactjson.ErrQuota;case "conflict":return zero,artifactjson.ErrConflict;case "reconcile":return zero,domain.NewError("ARCHIVE_BEGIN_RECONCILIATION_REQUIRED",409);case "created","replayed":default:return zero,artifactjson.ErrStale}
 if id==nil||*id!=intent.ID()||token==nil||*token==uuid.Nil||state==nil{return zero,objectstore.ErrIntegrity}
 result:=ArchiveUploadReservation{Token:*token,State:*state}
 if multipart!=nil{result.MultipartID=*multipart}
 switch result.State{
 case "reserved":if multipart!=nil{return zero,objectstore.ErrIntegrity}
 case "uploading","completing","verified":if !objectstore.ValidInterchangeMultipartID(result.MultipartID){return zero,objectstore.ErrIntegrity}
 default:return zero,objectstore.ErrIntegrity
 }
 if outcome=="created"&&result.State!="reserved"{return zero,objectstore.ErrIntegrity}
 return result,nil
}
func(s SQLArchiveUploadRepository)Reserve(ctx context.Context,who auth.Identity,tid uuid.UUID,intent ArchiveUploadIntent)(ArchiveUploadReservation,error){
 var result ArchiveUploadReservation
 if err:=s.validate(ctx,who,tid,intent);err!=nil{return result,err}
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var outcome string;var id,token *uuid.UUID;var state,multipart *string
  if err:=tx.QueryRow(ctx,`SELECT outcome,upload_id,token,state,multipart_id FROM photo.reserve_v30_archive_upload($1,$2,$3,$4,$5,$6)`,tid,who.UserID,who.SessionID,intent.key,string(intent.RequestBody()),s.Bucket).Scan(&outcome,&id,&token,&state,&multipart);err!=nil{return err}
  var err error;result,err=validateArchiveReservation(outcome,id,token,state,multipart,intent);return err
 })
 if err!=nil{return ArchiveUploadReservation{},err};if err:=ctx.Err();err!=nil{return ArchiveUploadReservation{},err};return result,nil
}
func(s SQLArchiveUploadRepository)Bind(ctx context.Context,who auth.Identity,tid uuid.UUID,intent ArchiveUploadIntent,token uuid.UUID,multipart string)error{
 if err:=s.validate(ctx,who,tid,intent);err!=nil{return err}
 if token==uuid.Nil||!objectstore.ValidInterchangeMultipartID(multipart){return v2input.ErrInvalid}
 return tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var accepted bool
  if err:=tx.QueryRow(ctx,`SELECT photo.bind_v30_archive_multipart($1,$2,$3,$4,$5,$6,$7)`,tid,who.UserID,who.SessionID,intent.ID(),token,intent.RequestSHA(),multipart).Scan(&accepted);err!=nil{return err}
  if !accepted{return artifactjson.ErrStale};return nil
 })
}
func(s SQLArchiveUploadRepository)Queue(ctx context.Context,who auth.Identity,tid uuid.UUID,intent ArchiveUploadIntent,parts []objectstore.UploadedPart)error{
 if err:=s.validate(ctx,who,tid,intent);err!=nil{return err}
 owned,err:=intent.CompletionParts(ctx,intent.Request().Bytes,parts);if err!=nil{return err}
 body,err:=canonical.Marshal(owned);if err!=nil{return err};if len(body)>1048576{return v2input.ErrInvalid}
 return tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var accepted bool
  if err:=tx.QueryRow(ctx,`SELECT photo.queue_v30_archive_completion($1,$2,$3,$4,$5,$6,$7::jsonb)`,tid,who.UserID,who.SessionID,intent.ID(),intent.RequestSHA(),intent.Request().Bytes,string(body)).Scan(&accepted);err!=nil{return err}
  if !accepted{return artifactjson.ErrStale};return nil
 })
}

func(s SQLArchiveUploadRepository)Abort(ctx context.Context,who auth.Identity,tid uuid.UUID,intent ArchiveUploadIntent)error{
 if err:=s.validate(ctx,who,tid,intent);err!=nil{return err}
 return tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var accepted bool
  if err:=tx.QueryRow(ctx,`SELECT photo.abort_v30_archive_upload($1,$2,$3,$4,$5)`,tid,who.UserID,who.SessionID,intent.ID(),intent.RequestSHA()).Scan(&accepted);err!=nil{return err}
  if !accepted{return artifactjson.ErrStale};return nil
 })
}
