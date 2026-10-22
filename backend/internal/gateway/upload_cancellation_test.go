package gateway

import (
 "bytes"
 "context"
 "crypto/x509"
 "errors"
 "io"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
)
type cancellationOutputRepository struct{base *outputRepository;afterBegin,afterComplete func()}
func(r cancellationOutputRepository)Begin(ctx context.Context,q UploadRequest)(UploadTicket,error){out,err:=r.base.Begin(ctx,q);if r.afterBegin!=nil{r.afterBegin()};return out,err}
func(r cancellationOutputRepository)Complete(ctx context.Context,q UploadRequest,ticket UploadTicket,o objectstore.Object)(ArtifactRef,error){out,err:=r.base.Complete(ctx,q,ticket,o);if r.afterComplete!=nil{r.afterComplete()};return out,err}
type cancellationOutputWriter struct{base *outputWriter;afterPut func()}
func(w cancellationOutputWriter)PutImmutable(ctx context.Context,key string,body io.Reader,size int64,sha,media string)(objectstore.Object,error){out,err:=w.base.PutImmutable(ctx,key,body,size,sha,media);if w.afterPut!=nil{w.afterPut()};return out,err}
func TestUploadCancellationDoesNotAdvanceOrReportLateSuccess(t *testing.T){
 for _,tc:=range []struct{phase string;begins,writes,completes int}{
  {"before-begin",0,0,0},{"after-begin",1,0,0},{"replay-after-begin",1,0,0},
  {"after-put",1,1,0},{"after-complete",1,1,1},
 }{t.Run(tc.phase,func(t *testing.T){
  _,identity,_,_:=fixture(t)
  q:=UploadRequest{Identity:identity,Certificate:&x509.Certificate{Raw:[]byte("verified-peer")},IdempotencyKey:uuid.New(),Role:"result",MediaType:"application/json",SHA256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",Bytes:2}
  artifact:=uuid.New();key,err:=objectstore.AttemptKey(identity.TenantID,identity.TaskID,artifact,identity.Attempt,identity.Fence);if err!=nil{t.Fatal(err)}
  base:=&outputRepository{ticket:UploadTicket{ArtifactID:artifact,UploadID:uuid.New(),ObjectKey:key}}
  writer:=&outputWriter{};ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  repository:=cancellationOutputRepository{base:base};objects:=cancellationOutputWriter{base:writer}
  switch tc.phase{
  case "before-begin":cancel()
  case "after-begin":repository.afterBegin=cancel
  case "replay-after-begin":base.ticket.Replay=&ArtifactRef{ArtifactID:artifact,SHA256:q.SHA256};repository.afterBegin=cancel
  case "after-put":objects.afterPut=cancel
  case "after-complete":repository.afterComplete=cancel
  }
  service:=UploadService{Repository:repository,Objects:objects}
  out,replay,err:=service.Upload(ctx,q,bytes.NewBufferString("{}"))
  if !errors.Is(err,context.Canceled)||replay||out.ArtifactID!=uuid.Nil{t.Fatal("canceled operation reported success",out,replay,err)}
  if base.begins!=tc.begins||writer.calls!=tc.writes||base.completes!=tc.completes{t.Fatal("canceled operation advanced",base.begins,writer.calls,base.completes)}
 })}
}
