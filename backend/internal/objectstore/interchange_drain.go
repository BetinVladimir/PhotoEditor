package objectstore
import (
 "context"
 "github.com/google/uuid"
 "github.com/minio/minio-go/v7"
)
// DrainInterchangeMultipart revokes the exact known multipart capability. Caller
// must fence new Begin/Complete writers and require no unresolved writer tickets.
// NoSuchUpload from ListParts is positive server evidence; a listing alone is not.
func(s *Store)DrainInterchangeMultipart(ctx context.Context,tenant,upload uuid.UUID,multipart string)error{
 key,err:=InterchangeUploadKey(tenant,upload);if err!=nil{return err}
 if s==nil||s.client==nil||s.bucket==""||(multipart!=""&&!ValidInterchangeMultipartID(multipart)){return ErrInvalid}
 core:=minio.Core{Client:s.client}
 return drainInterchangeMultipart(ctx,key,multipart,
  func(ctx context.Context)error{return core.AbortMultipartUpload(ctx,s.bucket,key,multipart)},
  func(ctx context.Context)error{_,err:=core.ListObjectParts(ctx,s.bucket,key,multipart,0,1);return err},
  func(ctx context.Context)<-chan minio.ObjectMultipartInfo{return s.client.ListIncompleteUploads(ctx,s.bucket,key,true)})
}
func drainInterchangeMultipart(ctx context.Context,key,multipart string,abort,probe func(context.Context)error,list func(context.Context)<-chan minio.ObjectMultipartInfo)error{
 if !validInterchangeUploadKey(key)||abort==nil||probe==nil||list==nil{return ErrInvalid}
 if err:=ctx.Err();err!=nil{return err}
 if multipart!=""{
  if err:=abort(ctx);err!=nil&&minio.ToErrorResponse(err).Code!="NoSuchUpload"{return classify(err)}
  err:=probe(ctx)
  if err==nil{return ErrUnavailable}
  if minio.ToErrorResponse(err).Code!="NoSuchUpload"{return classify(err)}
 }
 if err:=ctx.Err();err!=nil{return err}
 scan,cancel:=context.WithCancel(ctx);defer cancel();rows:=list(scan);if rows==nil{return ErrUnavailable}
 return awaitInterchangeMultipartAbsent(ctx,key,rows)
}
