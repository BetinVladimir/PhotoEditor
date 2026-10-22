package objectstore

import (
 "context"
 "errors"
 "net/http"
 "sync/atomic"
)

// A private call-local token prevents an SDK retry from issuing another
// non-idempotent S3 CreateMultipartUpload for one archive Begin invocation.
// An unknown first response still remains unknown; this is not a receipt or
// proof that an in-flight server request has terminated.
type interchangeCreateAttemptKey struct{}
var errInterchangeCreateRepeated=errors.New("archive multipart create already attempted")
var ErrInterchangeCreateNotDispatched=errors.New("archive multipart create was not dispatched")

func singleInterchangeCreateContext(ctx context.Context)context.Context{
 guarded,_:=trackedInterchangeCreateContext(ctx);return guarded
}
func trackedInterchangeCreateContext(ctx context.Context)(context.Context,*atomic.Bool){
 attempt:=new(atomic.Bool)
 return context.WithValue(ctx,interchangeCreateAttemptKey{},attempt),attempt
}

type singleMultipartCreateTransport struct{base http.RoundTripper}
func(t *singleMultipartCreateTransport)RoundTrip(request *http.Request)(*http.Response,error){
 if request==nil||t==nil||t.base==nil{return nil,ErrInvalid}
 refuse:=func(err error)(*http.Response,error){if request.Body!=nil{_ = request.Body.Close()};return nil,err}
 if err:=request.Context().Err();err!=nil{return refuse(err)}
 attempt,_:=request.Context().Value(interchangeCreateAttemptKey{}).(*atomic.Bool)
 if attempt!=nil&&request.Method==http.MethodPost&&request.URL!=nil{
  if _,create:=request.URL.Query()["uploads"];create&&!attempt.CompareAndSwap(false,true){return refuse(errInterchangeCreateRepeated)}
 }
 response,err:=t.base.RoundTrip(request)
 if outcome,ok:=request.Context().Value(stylePackageCreateOutcomeKey{}).(*stylePackageCreateOutcome);ok && request.Method==http.MethodPost && request.URL!=nil {
  if _,create:=request.URL.Query()["uploads"];create && err==nil && response!=nil {outcome.record(response.StatusCode)}
 }
 return response,err
}

// Preserve http.Client cleanup when wrapping the shared SDK transport.
func(t *singleMultipartCreateTransport)CloseIdleConnections(){
 if t==nil{return}
 if closer,ok:=t.base.(interface{CloseIdleConnections()});ok{closer.CloseIdleConnections()}
}
