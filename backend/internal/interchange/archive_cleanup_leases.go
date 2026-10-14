package interchange

import (
 "context"
 "errors"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/v2input"
)

// Cleanup is independent of user sessions: authority comes from a scheduler lease.
// The processor must also establish writer quiescence before destructive IO.
type ArchiveCleanupLease struct {
 Tenant,Upload,Token uuid.UUID
 Bucket,Key string
 Until,NotBefore time.Time
}
type ArchiveCleanupLeases interface {
 Claim(context.Context)(*ArchiveCleanupLease,error)
 Renew(context.Context,ArchiveCleanupLease)(bool,error)
 Release(context.Context,ArchiveCleanupLease)(bool,error)
 Finish(context.Context,ArchiveCleanupLease)(bool,error)
}
type SQLArchiveCleanupLeases struct {DB artifactjson.RowQuerier;Bucket string}
func(s SQLArchiveCleanupLeases)Claim(ctx context.Context)(*ArchiveCleanupLease,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if s.DB==nil||len(s.Bucket)<3||len(s.Bucket)>63{return nil,v2input.ErrInvalid}
 var lease ArchiveCleanupLease
 err:=s.DB.QueryRow(ctx,`SELECT tenant_id,id,cleanup_token,bucket,object_key,cleanup_lease_until,cleanup_not_before FROM photo.claim_v30_archive_cleanup()`).Scan(&lease.Tenant,&lease.Upload,&lease.Token,&lease.Bucket,&lease.Key,&lease.Until,&lease.NotBefore)
 if errors.Is(err,pgx.ErrNoRows){return nil,nil};if err!=nil{return nil,err}
 if err:=s.valid(ctx,lease);err!=nil{return nil,err}
 if lease.Until.IsZero()||lease.NotBefore.IsZero(){return nil,v2input.ErrInvalid}
 return &lease,ctx.Err()
}
func(s SQLArchiveCleanupLeases)valid(ctx context.Context,lease ArchiveCleanupLease)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.DB==nil||len(s.Bucket)<3||len(s.Bucket)>63||lease.Token==uuid.Nil||lease.Bucket!=s.Bucket{return v2input.ErrInvalid}
 if _,err:=archiveStorageGeneration(lease.Tenant,lease.Upload,lease.Key);err!=nil{return v2input.ErrInvalid}
 return nil
}
func(s SQLArchiveCleanupLeases)update(ctx context.Context,lease ArchiveCleanupLease,query string)(bool,error){
 if err:=s.valid(ctx,lease);err!=nil{return false,err}
 var accepted bool;err:=s.DB.QueryRow(ctx,query,lease.Tenant,lease.Upload,lease.Token).Scan(&accepted);return accepted,err
}
func(s SQLArchiveCleanupLeases)Renew(ctx context.Context,lease ArchiveCleanupLease)(bool,error){return s.update(ctx,lease,`SELECT photo.renew_v30_archive_cleanup($1,$2,$3)`)}
func(s SQLArchiveCleanupLeases)Release(ctx context.Context,lease ArchiveCleanupLease)(bool,error){return s.update(ctx,lease,`SELECT photo.release_v30_archive_cleanup($1,$2,$3)`)}
func(s SQLArchiveCleanupLeases)Finish(ctx context.Context,lease ArchiveCleanupLease)(bool,error){
 if err:=s.valid(ctx,lease);err!=nil{return false,err}
 var accepted bool;err:=s.DB.QueryRow(ctx,`SELECT photo.finish_v30_archive_cleanup($1,$2,$3,$4,$5)`,lease.Tenant,lease.Upload,lease.Token,lease.Bucket,lease.Key).Scan(&accepted);return accepted,err
}
var _ ArchiveCleanupLeases=SQLArchiveCleanupLeases{}
