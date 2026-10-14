package interchange
import (
 "context"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)
type ArchiveUploadRecord struct{
 Intent ArchiveUploadIntent
 Reservation ArchiveUploadReservation
 StorageKey string
 ExpiresAt time.Time
 ErrorCode string
 Ready bool
}
func(s SQLArchiveUploadRepository)Lookup(ctx context.Context,who auth.Identity,tid,pid,upload uuid.UUID)(ArchiveUploadRecord,error){
 var result ArchiveUploadRecord
 if err:=ctx.Err();err!=nil{return result,err}
 if s.DB==nil||len(s.Bucket)<3||len(s.Bucket)>63||tid==uuid.Nil||pid==uuid.Nil||upload==uuid.Nil{return result,v2input.ErrInvalid}
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var body,sha,key,bucket,objectKey string;var artifact uuid.UUID;var multipart,errorCode *string
  err:=tx.QueryRow(ctx,`SELECT request_body,request_sha,key,token,state,multipart_id,bucket,object_key,artifact_id,expires_at,error_code,archive_ready FROM photo.lookup_v30_archive_upload($1,$2,$3,$4,$5)`,tid,pid,who.UserID,who.SessionID,upload).Scan(&body,&sha,&key,&result.Reservation.Token,&result.Reservation.State,&multipart,&bucket,&objectKey,&artifact,&result.ExpiresAt,&errorCode,&result.Ready)
  if err==pgx.ErrNoRows{return artifactjson.ErrStale};if err!=nil{return err}
  if len(body)>16384||bucket!=s.Bucket||result.Reservation.Token==uuid.Nil||result.ExpiresAt.IsZero(){return objectstore.ErrIntegrity}
  var request ArchiveUploadRequest;if err:=decodeImportDocument([]byte(body),&request);err!=nil{return err}
  intent,err:=FreezeArchiveUpload(ctx,tid,who,key,request);if err!=nil{return err}
  if request.ProjectID!=pid||intent.ID()!=upload||intent.ArtifactID()!=artifact||intent.RequestSHA()!=sha||string(intent.RequestBody())!=body{return objectstore.ErrIntegrity}
  if _,err:=archiveStorageGeneration(tid,upload,objectKey);err!=nil{return objectstore.ErrIntegrity}
  switch result.Reservation.State{case "reserved","uploading","completing","verified","aborting","aborted","failed":default:return objectstore.ErrIntegrity}
  if multipart!=nil{if !objectstore.ValidInterchangeMultipartID(*multipart){return objectstore.ErrIntegrity};result.Reservation.MultipartID=*multipart}
  switch result.Reservation.State{
  case "reserved":if multipart!=nil{return objectstore.ErrIntegrity}
  case "uploading","completing","verified":if multipart==nil{return objectstore.ErrIntegrity}
  }
  if errorCode!=nil{switch *errorCode{case "ARCHIVE_INTEGRITY","UPLOAD_ABORTED","UPLOAD_EXPIRED":result.ErrorCode=*errorCode;default:return objectstore.ErrIntegrity}}
  if (result.Reservation.State=="aborting"||result.Reservation.State=="aborted")!=(result.ErrorCode!=""){return objectstore.ErrIntegrity}
  if result.Ready&&result.Reservation.State!="verified"{return objectstore.ErrIntegrity}
  result.Intent=intent;result.StorageKey=objectKey;return nil
 })
 if err!=nil{return ArchiveUploadRecord{},err};if err:=ctx.Err();err!=nil{return ArchiveUploadRecord{},err};return result,nil
}
