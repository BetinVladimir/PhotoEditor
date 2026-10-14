package objectstore

import (
 "context"
 "github.com/minio/minio-go/v7"
)

// An incomplete or oversized listing never establishes absence. Caller owns
// and cancels the producer context when this bounded consumer returns.
func awaitInterchangeMultipartAbsent(ctx context.Context,key string,rows <-chan minio.ObjectMultipartInfo)error{
 if !validInterchangeUploadKey(key)||rows==nil{return ErrInvalid}
 return awaitExactMultipartAbsent(ctx,key,rows)
}
func awaitExactMultipartAbsent(ctx context.Context,key string,rows <-chan minio.ObjectMultipartInfo)error{
 if key==""||rows==nil{return ErrInvalid}
 count:=0
 for {
  select {
  case <-ctx.Done():return ctx.Err()
  case row,ok:=<-rows:
   if !ok{return ctx.Err()}
   if err:=ctx.Err();err!=nil{return err}
   count++;if count>10000{return ErrUnavailable}
   if row.Err!=nil{return classify(row.Err)}
   if row.Key==key{return ErrUnavailable}
  }
 }
}
