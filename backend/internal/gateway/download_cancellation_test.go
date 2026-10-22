package gateway

import (
 "context"
 "crypto/tls"
 "crypto/x509"
 "io"
 "net/http"
 "net/http/httptest"
 "testing"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
)

type lateDownloadBody struct { read func(); called bool }
func(b *lateDownloadBody)Read(p []byte)(int,error){b.called=true;b.read();copy(p,"private");return 7,io.EOF}
func(b *lateDownloadBody)Close()error{return nil}
type lateDownloadObjects struct { body io.ReadCloser; opened int; onOpen func() }
func(o *lateDownloadObjects)Open(context.Context,objectstore.Object)(io.ReadCloser,error){o.opened++;if o.onOpen!=nil{o.onOpen()};return o.body,nil}
func(o *lateDownloadObjects)OpenRange(c context.Context,r objectstore.Object,_,_ int64)(io.ReadCloser,error){return o.Open(c,r)}

type deadlineDownloadRecorder struct { *httptest.ResponseRecorder; writeDeadline time.Time }
func(w *deadlineDownloadRecorder)SetWriteDeadline(deadline time.Time)error{if !deadline.IsZero(){w.writeDeadline=deadline};return nil}

func TestDownloadLateIOCannotExposeBytesAfterCancellationOrLeaseRevocation(t *testing.T){
 for _,phase:=range []string{"input","open","read_cancel","read_lease","live_deadline"}{t.Run(phase,func(t *testing.T){
  signer,id,cert,now:=fixture(t);artifact:=uuid.New()
  raw,err:=signer.Issue(id,cert,now.Add(time.Minute),now.Add(time.Hour),[]string{ReadObjects});if err!=nil{t.Fatal(err)}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  revoked:=false;leaseEnd:=time.Now().Add(250*time.Millisecond)
  body:=&lateDownloadBody{read:func(){if phase=="read_lease"{time.Sleep(time.Until(leaseEnd)+5*time.Millisecond);revoked=true}else if phase=="read_cancel"{cancel()}}}
  objects:=&lateDownloadObjects{body:body};if phase=="open"{objects.onOpen=cancel}
  h:=HTTP{Signer:signer,Bucket:"private",MaxDownloadBytes:100,Objects:objects,
   Tasks:resolverFunc(func(context.Context,uuid.UUID,int,int64,uuid.UUID,*x509.Certificate)(TaskContext,error){if revoked{return TaskContext{},ErrCapability};return TaskContext{Identity:id,LeaseUntil:leaseEnd,Deadline:time.Now().Add(time.Hour)},nil}),
   Inputs:inputFunc(func(context.Context,TaskIdentity,*x509.Certificate,uuid.UUID,string)(objectstore.Object,error){if phase=="input"{cancel()};return objectstore.Object{VersionID:"v1",Key:"private/key",Bytes:7,SHA256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",ContentType:"image/png"},nil})}
  r:=httptest.NewRequest(http.MethodGet,"/internal/v1/tasks/"+id.TaskID.String()+"/objects/"+artifact.String(),nil).WithContext(ctx)
  r.Header.Set("X-Photo-Worker",id.WorkerID.String());r.Header.Set("X-Photo-Attempt","2");r.Header.Set("X-Photo-Fence","9");r.Header.Set("Authorization","Bearer "+raw)
  r.TLS=&tls.ConnectionState{HandshakeComplete:true,PeerCertificates:[]*x509.Certificate{cert},VerifiedChains:[][]*x509.Certificate{{cert}}}
  w:=&deadlineDownloadRecorder{ResponseRecorder:httptest.NewRecorder()}
  func(){defer func(){if p:=recover();p!=nil&&p!=http.ErrAbortHandler{panic(p)}}();h.Handler().ServeHTTP(w,r)}()
  if phase!="live_deadline"&&w.Body.String()=="private"{t.Fatal("late IO disclosed source bytes")}
  if phase=="live_deadline"&&(w.Body.String()!="private"||w.writeDeadline.IsZero()||w.writeDeadline.After(leaseEnd)){t.Fatal("live write deadline exceeded lease")}
  if phase=="input"&&objects.opened!=0{t.Fatal("canceled lookup opened storage")}
  if (phase=="read_cancel"||phase=="read_lease")&&!body.called{t.Fatal("scenario did not reach blocking read")}
 })}
}
