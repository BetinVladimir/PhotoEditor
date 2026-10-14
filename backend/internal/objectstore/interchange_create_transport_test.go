package objectstore

import (
 "context"
 "errors"
 "io"
 "net/http"
 "strings"
 "sync"
 "sync/atomic"
 "testing"
 "time"
 "github.com/minio/minio-go/v7"
 "github.com/minio/minio-go/v7/pkg/credentials"
)

type archiveRoundTrip func(*http.Request)(*http.Response,error)
func(f archiveRoundTrip)RoundTrip(r *http.Request)(*http.Response,error){return f(r)}
type archiveRequestBody struct{io.Reader;closed bool}
func(b *archiveRequestBody)Close()error{b.closed=true;return nil}

func TestArchiveCreateTransportAllowsOnlyOneActualCreatePerInvocation(t *testing.T){
 for _,mode:=range []string{"success","unknown"}{t.Run(mode,func(t *testing.T){
  calls:=0
  transport:=&singleMultipartCreateTransport{base:archiveRoundTrip(func(*http.Request)(*http.Response,error){
   calls++;if mode=="unknown"{return nil,errors.New("response lost")}
   return &http.Response{StatusCode:200,Body:io.NopCloser(strings.NewReader("receipt"))},nil
  })}
  ctx:=singleInterchangeCreateContext(context.Background())
  first,_:=http.NewRequestWithContext(ctx,http.MethodPost,"https://storage.test/photos/archive.zip?uploads=",nil)
  response,_:=transport.RoundTrip(first);if response!=nil{response.Body.Close()}
  body:=&archiveRequestBody{Reader:strings.NewReader("")}
  second,_:=http.NewRequestWithContext(ctx,http.MethodPost,"https://storage.test/photos/archive.zip?uploads",body)
  if _,err:=transport.RoundTrip(second);!errors.Is(err,errInterchangeCreateRepeated)||calls!=1||!body.closed{t.Fatal("SDK retry reached storage",err,calls,body.closed)}
 })}
}

func TestArchiveCreateTransportKeepsOtherRequestsAndInvocationsIndependent(t *testing.T){
 calls:=0;transport:=&singleMultipartCreateTransport{base:archiveRoundTrip(func(*http.Request)(*http.Response,error){calls++;return nil,nil})}
 ctx:=singleInterchangeCreateContext(context.Background())
 for _,endpoint:=range []string{"?location=","?uploadId=receipt","?uploads="}{
  request,_:=http.NewRequestWithContext(ctx,http.MethodPost,"https://storage.test/photos/archive.zip"+endpoint,nil)
  if _,err:=transport.RoundTrip(request);err!=nil{t.Fatal(err)}
 }
 for _,next:=range []context.Context{context.Background(),singleInterchangeCreateContext(context.Background())}{
  request,_:=http.NewRequestWithContext(next,http.MethodPost,"https://storage.test/photos/archive.zip?uploads=",nil)
  if _,err:=transport.RoundTrip(request);err!=nil{t.Fatal(err)}
 }
 if calls!=5{t.Fatal("unrelated request blocked",calls)}
}

func TestArchiveCreateTransportConcurrentRetriesCannotDoubleCreate(t *testing.T){
 var calls atomic.Int32
 transport:=&singleMultipartCreateTransport{base:archiveRoundTrip(func(*http.Request)(*http.Response,error){calls.Add(1);return nil,nil})}
 ctx:=singleInterchangeCreateContext(context.Background());var pending sync.WaitGroup
 for i:=0;i<16;i++{pending.Add(1);go func(){defer pending.Done();request,_:=http.NewRequestWithContext(ctx,http.MethodPost,"https://storage.test/photos/archive.zip?uploads=",nil);_,_ = transport.RoundTrip(request)}()}
 pending.Wait();if calls.Load()!=1{t.Fatal("parallel retry issued multiple creates",calls.Load())}
}

func TestArchiveCreateTransportCancellationRefusesAndClosesBody(t *testing.T){
 ctx,cancel:=context.WithCancel(singleInterchangeCreateContext(context.Background()));cancel()
 body:=&archiveRequestBody{Reader:strings.NewReader("")}
 request,_:=http.NewRequestWithContext(ctx,http.MethodPost,"https://storage.test/photos/archive.zip?uploads=",body)
 transport:=&singleMultipartCreateTransport{base:archiveRoundTrip(func(*http.Request)(*http.Response,error){t.Fatal("cancelled create reached network");return nil,nil})}
 if _,err:=transport.RoundTrip(request);!errors.Is(err,context.Canceled)||!body.closed{t.Fatal(err,body.closed)}
}

func TestArchiveCreateTransportTracksDispatchBoundary(t *testing.T){
 guarded,attempt:=trackedInterchangeCreateContext(context.Background())
 transport:=&singleMultipartCreateTransport{base:archiveRoundTrip(func(*http.Request)(*http.Response,error){return nil,errors.New("response lost")})}
 request,_:=http.NewRequestWithContext(guarded,http.MethodPost,"https://storage.test/photos/archive.zip?uploads=",nil)
 if _,err:=transport.RoundTrip(request);err==nil||!attempt.Load(){t.Fatal("sent Create misclassified as not dispatched",err)}
 cancelled,cancel:=context.WithCancel(context.Background());cancel()
 guarded,attempt=trackedInterchangeCreateContext(cancelled)
 request,_=http.NewRequestWithContext(guarded,http.MethodPost,"https://storage.test/photos/archive.zip?uploads=",nil)
 if _,err:=transport.RoundTrip(request);!errors.Is(err,context.Canceled)||attempt.Load(){t.Fatal("cancelled Create marked dispatched",err)}
}


func TestArchiveCreateGuardWrapsActualSDKRetryPath(t *testing.T){
 calls:=0
 guarded:=&singleMultipartCreateTransport{base:archiveRoundTrip(func(request *http.Request)(*http.Response,error){
  calls++
  if request.Method!=http.MethodPost{t.Fatal("unexpected SDK method",request.Method)}
  if _,ok:=request.URL.Query()["uploads"];!ok{t.Fatal("unexpected SDK request")}
  return &http.Response{StatusCode:http.StatusServiceUnavailable,Header:http.Header{"Content-Type":[]string{"application/xml"}},Body:io.NopCloser(strings.NewReader("<Error><Code>ServiceUnavailable</Code><Message>retry</Message></Error>")),Request:request},nil
 })}
 client,err:=minio.New("storage.test",&minio.Options{Creds:credentials.NewStaticV4("fixture","fixture-secret",""),Region:"us-east-1",Transport:guarded})
 if err!=nil{t.Fatal(err)}
 ctx,cancel:=context.WithTimeout(singleInterchangeCreateContext(context.Background()),2*time.Second);defer cancel()
 if _,err:=(minio.Core{Client:client}).NewMultipartUpload(ctx,"photos","archive.zip",minio.PutObjectOptions{ContentType:"application/zip"});err==nil{t.Fatal("failed Create became successful")}
 if calls!=1{t.Fatal("SDK sent non-idempotent Create repeatedly",calls)}
}
