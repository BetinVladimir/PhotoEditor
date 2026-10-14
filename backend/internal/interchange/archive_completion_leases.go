package interchange
import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "io"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type ArchiveCompletionLease struct{
 Tenant,Upload,Token,Actor,Session uuid.UUID
 Intent ArchiveUploadIntent
 StorageKey string
 MultipartID string
 Parts []objectstore.UploadedPart
 Until time.Time
}
type ArchiveCompletionLeases interface{
 Claim(context.Context)(*ArchiveCompletionLease,error)
 Renew(context.Context,ArchiveCompletionLease)(bool,error)
 Release(context.Context,ArchiveCompletionLease)(bool,error)
 Fail(context.Context,ArchiveCompletionLease)(bool,error)
 Finish(context.Context,ArchiveCompletionLease,objectstore.Object)(bool,error)
}
type SQLArchiveCompletionLeases struct{DB artifactjson.RowQuerier;Bucket string;Recovery bool}
func(s SQLArchiveCompletionLeases)Claim(ctx context.Context)(*ArchiveCompletionLease,error){
 if err:=ctx.Err();err!=nil{return nil,err};if s.DB==nil||len(s.Bucket)<3||len(s.Bucket)>63{return nil,v2input.ErrInvalid}
 var lease ArchiveCompletionLease;var project,artifact uuid.UUID;var key,body,requestSHA,bucket,objectKey,expectedSHA,filename string;var size int64;var parts []byte
 claimFunction:="photo.claim_v30_archive_completion()";if s.Recovery{claimFunction="photo.claim_v30_archive_completion_recovery()"}
 err:=s.DB.QueryRow(ctx,`SELECT tenant_id,id,completion_token,actor_id,completion_session_id,project_id,artifact_id,key,request_body,request_sha,bucket,object_key,multipart_id,expected_bytes,expected_sha,filename,parts,completion_lease_until FROM `+claimFunction).Scan(&lease.Tenant,&lease.Upload,&lease.Token,&lease.Actor,&lease.Session,&project,&artifact,&key,&body,&requestSHA,&bucket,&objectKey,&lease.MultipartID,&size,&expectedSHA,&filename,&parts,&lease.Until)
 if errors.Is(err,pgx.ErrNoRows){return nil,nil};if err!=nil{return nil,err}
 if len(body)>16384||len(parts)>1048576||lease.Token==uuid.Nil||!objectstore.ValidInterchangeMultipartID(lease.MultipartID)||bucket!=s.Bucket||lease.Until.IsZero(){return nil,v2input.ErrInvalid}
 var request ArchiveUploadRequest;if err:=decodeImportDocument([]byte(body),&request);err!=nil{return nil,err}
 intent,err:=FreezeArchiveUpload(ctx,lease.Tenant,auth.Identity{UserID:lease.Actor,SessionID:lease.Session},key,request);if err!=nil{return nil,err}
 if intent.ID()!=lease.Upload||intent.ArtifactID()!=artifact||intent.project!=project||intent.RequestSHA()!=requestSHA||intent.request.Bytes!=size||intent.request.SHA256!=expectedSHA||intent.request.Filename!=filename{return nil,v2input.ErrInvalid}
 if _,err:=archiveStorageGeneration(lease.Tenant,lease.Upload,objectKey);err!=nil{return nil,objectstore.ErrIntegrity};lease.StorageKey=objectKey
 decoder:=json.NewDecoder(bytes.NewReader(parts));decoder.DisallowUnknownFields()
 if decoder.Decode(&lease.Parts)!=nil||decoder.Decode(new(any))!=io.EOF{return nil,v2input.ErrInvalid}
 lease.Parts,err=intent.CompletionParts(ctx,size,lease.Parts);if err!=nil{return nil,err};lease.Intent=intent
 return &lease,ctx.Err()
}
func(s SQLArchiveCompletionLeases)valid(ctx context.Context,lease ArchiveCompletionLease)error{
 if s.DB==nil||len(s.Bucket)<3||len(s.Bucket)>63||lease.Token==uuid.Nil||!objectstore.ValidInterchangeMultipartID(lease.MultipartID){return v2input.ErrInvalid}
 p,err:=FreezeArchiveUpload(ctx,lease.Tenant,auth.Identity{UserID:lease.Actor,SessionID:lease.Session},lease.Intent.key,lease.Intent.Request());if err!=nil{return err}
 if p.ID()!=lease.Upload||p.ID()!=lease.Intent.ID()||p.RequestSHA()!=lease.Intent.RequestSHA()||p.ObjectKey()!=lease.Intent.ObjectKey(){return v2input.ErrInvalid}
 if _,err:=archiveStorageGeneration(lease.Tenant,lease.Upload,lease.StorageKey);err!=nil{return v2input.ErrInvalid}
 _,err=p.CompletionParts(ctx,p.request.Bytes,lease.Parts);return err
}
func(s SQLArchiveCompletionLeases)update(ctx context.Context,lease ArchiveCompletionLease,query string)(bool,error){
 if err:=s.valid(ctx,lease);err!=nil{return false,err};var accepted bool
 err:=s.DB.QueryRow(ctx,query,lease.Tenant,lease.Upload,lease.Token).Scan(&accepted);return accepted,err
}
func(s SQLArchiveCompletionLeases)Renew(ctx context.Context,lease ArchiveCompletionLease)(bool,error){return s.update(ctx,lease,`SELECT photo.renew_v30_archive_completion($1,$2,$3)`)}
func(s SQLArchiveCompletionLeases)Release(ctx context.Context,lease ArchiveCompletionLease)(bool,error){return s.update(ctx,lease,`SELECT photo.release_v30_archive_completion($1,$2,$3)`)}
func(s SQLArchiveCompletionLeases)Finish(ctx context.Context,lease ArchiveCompletionLease,object objectstore.Object)(bool,error){
 if err:=s.valid(ctx,lease);err!=nil{return false,err}
 if object.Key!=lease.StorageKey||object.Bytes!=lease.Intent.request.Bytes||object.SHA256!=lease.Intent.request.SHA256||object.ContentType!="application/zip"||!objectstore.ValidInterchangeVersionID(object.VersionID){return false,objectstore.ErrIntegrity}
 var accepted bool;err:=s.DB.QueryRow(ctx,`SELECT photo.finish_v30_archive_completion($1,$2,$3,$4,$5,$6,$7,$8)`,lease.Tenant,lease.Upload,lease.Token,s.Bucket,object.Key,object.VersionID,object.Bytes,object.SHA256).Scan(&accepted);return accepted,err
}
var _ ArchiveCompletionLeases=SQLArchiveCompletionLeases{}

func(s SQLArchiveCompletionLeases)Fail(ctx context.Context,lease ArchiveCompletionLease)(bool,error){return s.update(ctx,lease,`SELECT photo.fail_v30_archive_completion($1,$2,$3)`)}
