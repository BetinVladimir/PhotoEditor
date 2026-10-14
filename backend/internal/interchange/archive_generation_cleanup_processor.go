package interchange

import (
 "context"
 "errors"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type ArchiveGenerationCleanupLease struct {
 Tenant,Upload,Generation,Token uuid.UUID
 Bucket,Key,MultipartID string
 Until time.Time
}
func(l ArchiveGenerationCleanupLease)valid(bucket string)bool{
 if l.Tenant==uuid.Nil||l.Upload==uuid.Nil||l.Generation==uuid.Nil||l.Token==uuid.Nil||l.Bucket!=bucket||!objectstore.ValidInterchangeMultipartID(l.MultipartID)||l.Until.IsZero(){return false}
 key,err:=objectstore.InterchangeGenerationKey(l.Tenant,l.Upload,l.Generation)
 return err==nil&&l.Key==key
}
type ArchiveGenerationCleanupLeases interface{
 Claim(context.Context)(*ArchiveGenerationCleanupLease,error)
 Renew(context.Context,ArchiveGenerationCleanupLease)(bool,error)
 Release(context.Context,ArchiveGenerationCleanupLease)(bool,error)
 Finish(context.Context,ArchiveGenerationCleanupLease)(bool,error)
 Authorize(context.Context,ArchiveGenerationCleanupLease)(bool,error)
}
type SQLArchiveGenerationCleanupLeases struct{DB artifactjson.RowQuerier;Bucket string}
func(s SQLArchiveGenerationCleanupLeases)Claim(ctx context.Context)(*ArchiveGenerationCleanupLease,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if s.DB==nil||len(s.Bucket)<3||len(s.Bucket)>63{return nil,v2input.ErrInvalid}
 var lease ArchiveGenerationCleanupLease
 err:=s.DB.QueryRow(ctx,`SELECT tenant_id,upload_id,generation_id,cleanup_token,bucket,object_key,multipart_id,cleanup_lease_until FROM photo.claim_v30_archive_generation_cleanup()`).Scan(&lease.Tenant,&lease.Upload,&lease.Generation,&lease.Token,&lease.Bucket,&lease.Key,&lease.MultipartID,&lease.Until)
 if errors.Is(err,pgx.ErrNoRows){return nil,nil};if err!=nil{return nil,err}
 if !lease.valid(s.Bucket){return nil,objectstore.ErrIntegrity}
 return &lease,ctx.Err()
}
func(s SQLArchiveGenerationCleanupLeases)query(ctx context.Context,lease ArchiveGenerationCleanupLease,sql string)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err}
 if s.DB==nil||!lease.valid(s.Bucket){return false,v2input.ErrInvalid}
 var accepted bool
 err:=s.DB.QueryRow(ctx,sql,lease.Tenant,lease.Upload,lease.Generation,lease.Token,lease.Bucket,lease.Key,lease.MultipartID).Scan(&accepted)
 return accepted,err
}
func(s SQLArchiveGenerationCleanupLeases)Authorize(ctx context.Context,lease ArchiveGenerationCleanupLease)(bool,error){return s.query(ctx,lease,`SELECT photo.v30_archive_generation_cleanup_authorized($1,$2,$3,$4,$5,$6,$7)`)}
func(s SQLArchiveGenerationCleanupLeases)Renew(ctx context.Context,lease ArchiveGenerationCleanupLease)(bool,error){return s.query(ctx,lease,`SELECT photo.renew_v30_archive_generation_cleanup($1,$2,$3,$4,$5,$6,$7)`)}
func(s SQLArchiveGenerationCleanupLeases)Finish(ctx context.Context,lease ArchiveGenerationCleanupLease)(bool,error){return s.query(ctx,lease,`SELECT photo.finish_v30_archive_generation_cleanup($1,$2,$3,$4,$5,$6,$7)`)}
func(s SQLArchiveGenerationCleanupLeases)Release(ctx context.Context,lease ArchiveGenerationCleanupLease)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err}
 if s.DB==nil||!lease.valid(s.Bucket){return false,v2input.ErrInvalid}
 var accepted bool
 err:=s.DB.QueryRow(ctx,`SELECT photo.release_v30_archive_generation_cleanup($1,$2,$3,$4)`,lease.Tenant,lease.Upload,lease.Generation,lease.Token).Scan(&accepted)
 return accepted,err
}

type ArchiveGenerationCleanupObjects interface{
 DrainInterchangeGenerationMultipart(context.Context,uuid.UUID,uuid.UUID,uuid.UUID,string)error
 PurgeInterchangeGeneration(context.Context,uuid.UUID,uuid.UUID,uuid.UUID)error
}
type ArchiveGenerationCleanupProcessor struct{
 Leases ArchiveGenerationCleanupLeases
 Objects ArchiveGenerationCleanupObjects
 HeartbeatInterval,RenewTimeout,WorkTimeout time.Duration
}
func(p ArchiveGenerationCleanupProcessor)ProcessOne(ctx context.Context)(bool,error){
 interval,renewTimeout,workTimeout:=p.HeartbeatInterval,p.RenewTimeout,p.WorkTimeout
 if interval==0{interval=10*time.Second};if renewTimeout==0{renewTimeout=5*time.Second};if workTimeout==0{workTimeout=10*time.Minute}
 if p.Leases==nil||p.Objects==nil||interval<=0||interval>10*time.Second||renewTimeout<=0||renewTimeout>5*time.Second||workTimeout<=0||workTimeout>10*time.Minute{return false,v2input.ErrInvalid}
 claim,done:=context.WithTimeout(ctx,5*time.Second);borrowed,err:=p.Leases.Claim(claim);done();if borrowed==nil{return false,err}
 lease:=*borrowed;committed:=false
 defer func(){if !committed{release,stop:=context.WithTimeout(context.WithoutCancel(ctx),5*time.Second);defer stop();_,_=p.Leases.Release(release,lease)}}()
 if err!=nil{return false,err};if err:=ctx.Err();err!=nil{return false,err}
 if !lease.valid(lease.Bucket)||len(lease.Bucket)<3||len(lease.Bucket)>63{return false,objectstore.ErrIntegrity}
 work,stop:=context.WithTimeout(ctx,workTimeout);defer stop();work,lose:=context.WithCancelCause(work);defer lose(nil)
 renew:=func()error{check,cancel:=context.WithTimeout(work,renewTimeout);defer cancel();ok,err:=p.Leases.Renew(check,lease);if err!=nil{return err};if !ok{return ErrArchiveCleanupLeaseLost};return nil}
 authorize:=func()error{check,cancel:=context.WithTimeout(work,renewTimeout);defer cancel();ok,err:=p.Leases.Authorize(check,lease);if err!=nil{return err};if !ok{return ErrArchiveCleanupLeaseLost};return nil}
 if err:=renew();err!=nil{return false,err}
 heartbeat,stopHeartbeat:=context.WithCancel(work);joined:=make(chan struct{})
 go func(){defer close(joined);ticker:=time.NewTicker(interval);defer ticker.Stop();for{select{case <-heartbeat.Done():return;case <-ticker.C:if err:=renew();err!=nil{lose(err);return}}}}()
 defer func(){stopHeartbeat();<-joined}()
 if err:=authorize();err!=nil{return false,err}
 if err:=work.Err();err!=nil{return false,err}
 if err:=p.Objects.DrainInterchangeGenerationMultipart(work,lease.Tenant,lease.Upload,lease.Generation,lease.MultipartID);err!=nil{return false,err}
 if err:=authorize();err!=nil{return false,err}
 if err:=work.Err();err!=nil{return false,err}
 if err:=p.Objects.PurgeInterchangeGeneration(work,lease.Tenant,lease.Upload,lease.Generation);err!=nil{return false,err}
 if err:=authorize();err!=nil{return false,err}
 stopHeartbeat();<-joined
 if cause:=context.Cause(work);cause!=nil{return false,cause}
 if err:=renew();err!=nil{return false,err}
 if cause:=context.Cause(work);cause!=nil{return false,cause}
 finish,cancel:=context.WithTimeout(work,5*time.Second);accepted,err:=p.Leases.Finish(finish,lease);cancel()
 if err!=nil{return false,err};if !accepted{return false,ErrArchiveCleanupLeaseLost}
 if cause:=context.Cause(work);cause!=nil{return false,cause}
 committed=true;return true,nil
}
