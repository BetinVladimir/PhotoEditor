package objectstore
import (
 "context"
 "github.com/minio/minio-go/v7"
)
// ObserveInterchangeCompletion is read-only. Both probes must report that the
// exact multipart capability is absent before, between and after stable
// versioned object reads.
// Live/missing/ambiguous results retain the open writer; this never retries Complete.
func(s *Store)ObserveInterchangeCompletion(ctx context.Context,key,multipart string,size int64)(PendingObject,error){
 if s==nil||s.client==nil||s.bucket==""||!validInterchangeArchiveKey(key)||!ValidInterchangeMultipartID(multipart)||size<22||size>MaxInterchangeArchiveBytes{return PendingObject{},ErrInvalid}
 core:=minio.Core{Client:s.client}
 return observeInterchangeCompletion(ctx,key,size,
  func(ctx context.Context)error{_,err:=core.ListObjectParts(ctx,s.bucket,key,multipart,0,1);return err},
  func(ctx context.Context)(PendingObject,error){return s.RecoverInterchangeMultipart(ctx,key,size)})
}
func observeInterchangeCompletion(ctx context.Context,key string,size int64,probe func(context.Context)error,read func(context.Context)(PendingObject,error))(PendingObject,error){
 var zero PendingObject
 if !validInterchangeArchiveKey(key)||size<22||size>MaxInterchangeArchiveBytes||probe==nil||read==nil{return zero,ErrInvalid}
 absent:=func()error{if err:=ctx.Err();err!=nil{return err};err:=probe(ctx);if err==nil{return ErrUnavailable};if minio.ToErrorResponse(err).Code!="NoSuchUpload"{return classify(err)};return ctx.Err()}
 if err:=absent();err!=nil{return zero,err}
 first,err:=read(ctx);if err!=nil{return zero,err}
 if first.Key!=key||first.Bytes!=size||first.ContentType!="application/zip"||!ValidInterchangeVersionID(first.VersionID){return zero,ErrIntegrity}
 if err:=absent();err!=nil{return zero,err}
 second,err:=read(ctx);if err!=nil{return zero,err}
 if first!=second{return zero,ErrIntegrity}
 if err:=absent();err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 return first,nil
}
