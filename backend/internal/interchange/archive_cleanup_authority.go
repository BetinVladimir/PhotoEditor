package interchange
import (
 "context"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/v2input"
)
// ArchiveStorageDrain must come from the installed storage writer coordinator.
// It attests that no authorized Begin/Complete/part writer can still create bytes
// at this exact bucket/key. No elapsed-time-only implementation is acceptable.
type ArchiveStorageDrain interface{AssertArchiveDrained(context.Context,uuid.UUID,uuid.UUID,string,string)error}
type ArchiveCleanupAuthority struct{DB artifactjson.RowQuerier;Bucket string;Drain ArchiveStorageDrain}
func(s ArchiveCleanupAuthority)AssertQuiescent(ctx context.Context,lease ArchiveCleanupLease)error{
 if err:=ctx.Err();err!=nil{return err}
 _,err:=archiveStorageGeneration(lease.Tenant,lease.Upload,lease.Key)
 if err!=nil||s.DB==nil||s.Drain==nil||len(s.Bucket)<3||len(s.Bucket)>63||lease.Bucket!=s.Bucket||lease.Token==uuid.Nil{return v2input.ErrInvalid}
 bounded,stop:=context.WithTimeout(ctx,5*time.Second);defer stop()
 check:=func()error{
  var allowed bool
  if err:=s.DB.QueryRow(bounded,`SELECT photo.v30_archive_cleanup_authorized($1,$2,$3,$4,$5)`,lease.Tenant,lease.Upload,lease.Token,lease.Bucket,lease.Key).Scan(&allowed);err!=nil{return err}
  if !allowed{return ErrArchiveCleanupLeaseLost};return bounded.Err()
 }
 if err:=check();err!=nil{return err}
 if err:=s.Drain.AssertArchiveDrained(bounded,lease.Tenant,lease.Upload,lease.Bucket,lease.Key);err!=nil{return err}
 return check()
}
var _ ArchiveCleanupWriters=ArchiveCleanupAuthority{}
