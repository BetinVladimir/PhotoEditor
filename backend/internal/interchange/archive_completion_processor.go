package interchange
import (
 "bytes"
 "context"
 "errors"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
var ErrArchiveCompletionLeaseLost=errors.New("archive completion lease lost")
type ArchiveCompletionObjects interface{
 CompleteInterchangeMultipart(context.Context,string,string,int64,[]objectstore.UploadedPart)(objectstore.PendingObject,error)
 ObserveInterchangeCompletion(context.Context,string,string,int64)(objectstore.PendingObject,error)
 VerifyInterchangePending(context.Context,objectstore.PendingObject,string)(objectstore.Object,error)
}
type ArchiveCompletionProcessor struct{
 Leases ArchiveCompletionLeases
 Writers ArchiveCompleteWriters
 Objects ArchiveCompletionObjects
 ObserveOnly bool
 HeartbeatInterval,RenewTimeout,WorkTimeout time.Duration
}
func(p ArchiveCompletionProcessor)ProcessOne(ctx context.Context)(bool,error){
 interval,renewTimeout,workTimeout:=p.HeartbeatInterval,p.RenewTimeout,p.WorkTimeout
 if interval==0{interval=10*time.Second};if renewTimeout==0{renewTimeout=5*time.Second};if workTimeout==0{workTimeout=2*time.Hour}
 if p.Leases==nil||p.Objects==nil||p.Writers==nil||interval<=0||interval>10*time.Second||renewTimeout<=0||renewTimeout>5*time.Second||workTimeout<=0||workTimeout>2*time.Hour{return false,v2input.ErrInvalid}
 claim,cancel:=context.WithTimeout(ctx,5*time.Second);borrowed,err:=p.Leases.Claim(claim);cancel();if err!=nil||borrowed==nil{return false,err}
 lease:=*borrowed;lease.Intent.requestJCS=borrowed.Intent.RequestBody();lease.Parts=append([]objectstore.UploadedPart(nil),borrowed.Parts...)
 committed:=false
 defer func(){if !committed{release,done:=context.WithTimeout(context.WithoutCancel(ctx),5*time.Second);defer done();_,_=p.Leases.Release(release,lease)}}()
 if lease.Tenant==uuid.Nil||lease.Upload==uuid.Nil||lease.Token==uuid.Nil||lease.Actor==uuid.Nil||lease.Session==uuid.Nil||!objectstore.ValidInterchangeMultipartID(lease.MultipartID){return false,objectstore.ErrIntegrity}
 frozen,err:=FreezeArchiveUpload(ctx,lease.Tenant,auth.Identity{UserID:lease.Actor,SessionID:lease.Session},lease.Intent.key,lease.Intent.Request());if err!=nil{return false,err}
 if frozen.ID()!=lease.Upload||frozen.ID()!=lease.Intent.ID()||frozen.ArtifactID()!=lease.Intent.ArtifactID()||frozen.ObjectKey()!=lease.Intent.ObjectKey()||frozen.RequestSHA()!=lease.Intent.RequestSHA()||!bytes.Equal(frozen.RequestBody(),lease.Intent.RequestBody()){return false,objectstore.ErrIntegrity}
 if _,err:=archiveStorageGeneration(lease.Tenant,lease.Upload,lease.StorageKey);err!=nil{return false,objectstore.ErrIntegrity}
 parts,err:=lease.Intent.CompletionParts(ctx,lease.Intent.request.Bytes,lease.Parts);if err!=nil{return false,err}
 work,stop:=context.WithTimeout(ctx,workTimeout);defer stop();work,lose:=context.WithCancelCause(work);defer lose(nil)
 renew:=func()error{check,done:=context.WithTimeout(work,renewTimeout);defer done();ok,err:=p.Leases.Renew(check,lease);if err!=nil{return err};if !ok{return ErrArchiveCompletionLeaseLost};return nil}
 if err:=renew();err!=nil{return false,err}
 recoveryCtx,recoveryDone:=context.WithTimeout(work,5*time.Second)
 version,err:=p.Writers.RecoverComplete(recoveryCtx,lease);recoveryDone();if err!=nil{return false,err}
 var ticket ArchiveWriterTicket
 if version==""{
  startCtx,startDone:=context.WithTimeout(work,5*time.Second)
  ticket,err=p.Writers.StartComplete(startCtx,lease);startDone();if err!=nil{return false,err}
  if ticket.tenant!=lease.Tenant||ticket.upload!=lease.Upload||ticket.token==uuid.Nil||ticket.completion!=lease.Token||ticket.kind!="complete"||ticket.key!=lease.StorageKey||ticket.size!=lease.Intent.Request().Bytes{return false,objectstore.ErrIntegrity}
 }else if !objectstore.ValidInterchangeVersionID(version){return false,objectstore.ErrIntegrity}
 heartbeat,stopHeartbeat:=context.WithCancel(work);joined:=make(chan struct{})
 go func(){defer close(joined);ticker:=time.NewTicker(interval);defer ticker.Stop();for{select{case <-heartbeat.Done():return;case <-ticker.C:if err:=renew();err!=nil{lose(err);return}}}}()
 // Own the heartbeat even if an object-store adapter panics. Stop/join must
 // precede deferred lease release so a stale renew cannot race recovery.
 defer func(){stopHeartbeat();<-joined}()
 // A closed receipt pins a specific version; never recover the latest key
 // or repeat Complete in this branch. Verify reads that exact version below.
 pending:=objectstore.PendingObject{Key:lease.StorageKey,Bytes:lease.Intent.request.Bytes,ContentType:"application/zip",VersionID:version}
 var ioErr error
 if version==""{
  if p.ObserveOnly{pending,ioErr=p.Objects.ObserveInterchangeCompletion(work,lease.StorageKey,lease.MultipartID,lease.Intent.request.Bytes)}else{
   pending,ioErr=p.Objects.CompleteInterchangeMultipart(work,lease.StorageKey,lease.MultipartID,lease.Intent.request.Bytes,parts)
   if ioErr!=nil&&work.Err()==nil{pending,ioErr=p.Objects.ObserveInterchangeCompletion(work,lease.StorageKey,lease.MultipartID,lease.Intent.request.Bytes)}
  }
 }
 if ioErr==nil&&(pending.Key!=lease.StorageKey||pending.Bytes!=lease.Intent.request.Bytes||pending.ContentType!="application/zip"||!objectstore.ValidInterchangeVersionID(pending.VersionID)){ioErr=objectstore.ErrIntegrity}
 // Close only after observing a concrete completed version. Verification is
 // read-only; corrupt SHA can safely enter cleanup after the writer is closed.
 if ioErr==nil&&version==""{
  closeCtx,closeDone:=context.WithTimeout(context.WithoutCancel(ctx),5*time.Second)
  ioErr=p.Writers.CloseComplete(closeCtx,ticket,pending);closeDone()
 }
 var object objectstore.Object
 if ioErr==nil{object,ioErr=p.Objects.VerifyInterchangePending(work,pending,lease.Intent.request.SHA256)}
 stopHeartbeat();<-joined
 if cause:=context.Cause(work);cause!=nil{return false,cause}
 if ioErr==nil&&(object.Key!=pending.Key||object.VersionID!=pending.VersionID||object.Bytes!=pending.Bytes||object.ContentType!=pending.ContentType||object.SHA256!=lease.Intent.request.SHA256){ioErr=objectstore.ErrIntegrity}
 if errors.Is(ioErr,objectstore.ErrIntegrity){
  if err:=renew();err!=nil{return false,err}
  failure,done:=context.WithTimeout(work,5*time.Second);accepted,err:=p.Leases.Fail(failure,lease);done()
  if err!=nil{return false,err};if !accepted{return false,ErrArchiveCompletionLeaseLost}
  committed=true;return true,nil
 }
 if ioErr!=nil{return false,ioErr}
 // Renew once after the long stream read and before the atomic commit.
 if err:=renew();err!=nil{return false,err}
 finish,done:=context.WithTimeout(work,5*time.Second);accepted,err:=p.Leases.Finish(finish,lease,object);done()
 if err!=nil{return false,err};if !accepted{return false,ErrArchiveCompletionLeaseLost}
 committed=true;return true,nil
}
