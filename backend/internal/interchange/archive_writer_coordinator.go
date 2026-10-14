package interchange
import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)
type ArchiveWriterTicket struct{tenant,upload,token,completion uuid.UUID;actor auth.Identity;kind,key string;size int64}
type ArchiveBeginWriters interface{
 RecoverBegin(context.Context,auth.Identity,uuid.UUID,ArchiveUploadIntent,uuid.UUID)(string,error)
 StartBegin(context.Context,auth.Identity,uuid.UUID,ArchiveUploadIntent,uuid.UUID)(ArchiveWriterTicket,error)
 CloseBegin(context.Context,ArchiveWriterTicket,string)error
}
type SQLArchiveBeginWriters struct{DB *pgxpool.Pool;Bucket string}
func(s SQLArchiveBeginWriters)StartBegin(ctx context.Context,who auth.Identity,tid uuid.UUID,p ArchiveUploadIntent,reservation uuid.UUID)(ArchiveWriterTicket,error){
 var zero ArchiveWriterTicket
 repo:=SQLArchiveUploadRepository{DB:s.DB,Bucket:s.Bucket};if err:=repo.validate(ctx,who,tid,p);err!=nil{return zero,err};if reservation==uuid.Nil{return zero,v2input.ErrInvalid}
 var token *uuid.UUID
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  if err:=tx.QueryRow(ctx,`SELECT photo.start_v30_archive_begin($1,$2,$3,$4,$5)`,tid,who.UserID,who.SessionID,p.ID(),reservation).Scan(&token);err!=nil{return err}
  if token==nil||*token==uuid.Nil{return artifactjson.ErrStale};return nil
 })
 if err!=nil{return zero,err};if err:=ctx.Err();err!=nil{return zero,err}
 return ArchiveWriterTicket{tenant:tid,upload:p.ID(),token:*token,actor:who,kind:"begin"},nil
}
func(s SQLArchiveBeginWriters)CloseBegin(ctx context.Context,ticket ArchiveWriterTicket,multipart string)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.DB==nil||ticket.tenant==uuid.Nil||ticket.upload==uuid.Nil||ticket.token==uuid.Nil||ticket.kind!="begin"||!objectstore.ValidInterchangeMultipartID(multipart){return v2input.ErrInvalid}
 return tenant.WithTenant(ctx,s.DB,ticket.actor,ticket.tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var closed bool
  if err:=tx.QueryRow(ctx,`SELECT photo.finish_v30_archive_begin_writer($1,$2,$3,$4)`,ticket.tenant,ticket.upload,ticket.token,multipart).Scan(&closed);err!=nil{return err}
  if !closed{return artifactjson.ErrStale};return nil
 })
}
var _ ArchiveBeginWriters=SQLArchiveBeginWriters{}

type ArchiveCompleteWriters interface{
 RecoverComplete(context.Context,ArchiveCompletionLease)(string,error)
 StartComplete(context.Context,ArchiveCompletionLease)(ArchiveWriterTicket,error)
 CloseComplete(context.Context,ArchiveWriterTicket,objectstore.PendingObject)error
}
type SQLArchiveCompleteWriters struct{DB artifactjson.RowQuerier;Bucket string}
func(s SQLArchiveCompleteWriters)StartComplete(ctx context.Context,lease ArchiveCompletionLease)(ArchiveWriterTicket,error){
 var zero ArchiveWriterTicket
 if err:=(SQLArchiveCompletionLeases{DB:s.DB,Bucket:s.Bucket}).valid(ctx,lease);err!=nil{return zero,err}
 var token *uuid.UUID
 if err:=s.DB.QueryRow(ctx,`SELECT photo.start_v30_archive_complete($1,$2,$3)`,lease.Tenant,lease.Upload,lease.Token).Scan(&token);err!=nil{return zero,err}
 if token==nil||*token==uuid.Nil{return zero,artifactjson.ErrStale}
 if err:=ctx.Err();err!=nil{return zero,err}
 return ArchiveWriterTicket{tenant:lease.Tenant,upload:lease.Upload,token:*token,completion:lease.Token,kind:"complete",key:lease.StorageKey,size:lease.Intent.Request().Bytes},nil
}
func(s SQLArchiveCompleteWriters)CloseComplete(ctx context.Context,ticket ArchiveWriterTicket,pending objectstore.PendingObject)error{
 if err:=ctx.Err();err!=nil{return err}
 _,err:=archiveStorageGeneration(ticket.tenant,ticket.upload,ticket.key)
 if err!=nil||s.DB==nil||ticket.token==uuid.Nil||ticket.completion==uuid.Nil||ticket.kind!="complete"||pending.Key!=ticket.key||pending.Bytes!=ticket.size||pending.ContentType!="application/zip"||!objectstore.ValidInterchangeVersionID(pending.VersionID){return objectstore.ErrIntegrity}
 var closed bool
 if err:=s.DB.QueryRow(ctx,`SELECT photo.finish_v30_archive_complete_writer($1,$2,$3,$4,$5)`,ticket.tenant,ticket.upload,ticket.token,ticket.completion,pending.VersionID).Scan(&closed);err!=nil{return err}
 if !closed{return artifactjson.ErrStale};return ctx.Err()
}
var _ ArchiveCompleteWriters=SQLArchiveCompleteWriters{}

func(s SQLArchiveBeginWriters)RecoverBegin(ctx context.Context,who auth.Identity,tid uuid.UUID,p ArchiveUploadIntent,reservation uuid.UUID)(string,error){
 repo:=SQLArchiveUploadRepository{DB:s.DB,Bucket:s.Bucket};if err:=repo.validate(ctx,who,tid,p);err!=nil{return "",err};if reservation==uuid.Nil{return "",v2input.ErrInvalid}
 var multipart *string
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  if err:=tx.QueryRow(ctx,`SELECT photo.recover_v30_archive_begin($1,$2,$3,$4,$5)`,tid,who.UserID,who.SessionID,p.ID(),reservation).Scan(&multipart);err!=nil{return err}
  if multipart!=nil&&(!objectstore.ValidInterchangeMultipartID(*multipart)){return v2input.ErrInvalid};return nil
 })
 if err!=nil{return "",err};if err:=ctx.Err();err!=nil{return "",err};if multipart==nil{return "",nil};return *multipart,nil
}

func(s SQLArchiveCompleteWriters)RecoverComplete(ctx context.Context,lease ArchiveCompletionLease)(string,error){
 if err:=(SQLArchiveCompletionLeases{DB:s.DB,Bucket:s.Bucket}).valid(ctx,lease);err!=nil{return "",err}
 var version *string
 if err:=s.DB.QueryRow(ctx,`SELECT photo.recover_v30_archive_complete($1,$2,$3)`,lease.Tenant,lease.Upload,lease.Token).Scan(&version);err!=nil{return "",err}
 if err:=ctx.Err();err!=nil{return "",err};if version==nil{return "",nil}
 if !objectstore.ValidInterchangeVersionID(*version){return "",objectstore.ErrIntegrity}
 return *version,nil
}

// Recovery reuses only an existing open ticket; it never calls StartComplete SQL.
type SQLArchiveRecoveryWriters struct{SQLArchiveCompleteWriters}
func(s SQLArchiveRecoveryWriters)StartComplete(ctx context.Context,lease ArchiveCompletionLease)(ArchiveWriterTicket,error){
 var zero ArchiveWriterTicket
 if err:=(SQLArchiveCompletionLeases{DB:s.DB,Bucket:s.Bucket}).valid(ctx,lease);err!=nil{return zero,err}
 var token *uuid.UUID
 if err:=s.DB.QueryRow(ctx,`SELECT photo.adopt_v30_archive_complete_writer($1,$2,$3)`,lease.Tenant,lease.Upload,lease.Token).Scan(&token);err!=nil{return zero,err}
 if token==nil||*token==uuid.Nil{return zero,artifactjson.ErrStale};if err:=ctx.Err();err!=nil{return zero,err}
 return ArchiveWriterTicket{tenant:lease.Tenant,upload:lease.Upload,token:*token,completion:lease.Token,kind:"complete",key:lease.StorageKey,size:lease.Intent.Request().Bytes},nil
}
var _ ArchiveCompleteWriters=SQLArchiveRecoveryWriters{}

