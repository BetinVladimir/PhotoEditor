package objectstore

import (
 "context"
 "github.com/google/uuid"
 "github.com/minio/minio-go/v7"
)

// PurgeInterchangeUpload requires an exclusive cleanup lease and fenced writers.
// It removes only a generated archive key, never a user-supplied prefix.
// Success is storage evidence only; the caller must separately fence quota release.
func(s *Store)PurgeInterchangeUpload(ctx context.Context,tenant,upload uuid.UUID)error{
 key,err:=InterchangeUploadKey(tenant,upload);if err!=nil{return err}
 if s==nil||s.client==nil||s.bucket==""{return ErrInvalid}
 return purgeInterchangeKey(ctx,key,s.purgeReservedKey,func(ctx context.Context,key string)<-chan minio.ObjectMultipartInfo{
  return s.client.ListIncompleteUploads(ctx,s.bucket,key,true)
 })
}

func purgeInterchangeKey(ctx context.Context,key string,purge func(context.Context,string)error,uploads func(context.Context,string)<-chan minio.ObjectMultipartInfo)error{
 if !validInterchangeUploadKey(key)||purge==nil||uploads==nil{return ErrInvalid}
 return purgeExactInterchangeKey(ctx,key,purge,uploads)
}

func purgeExactInterchangeKey(ctx context.Context,key string,purge func(context.Context,string)error,uploads func(context.Context,string)<-chan minio.ObjectMultipartInfo)error{
 if key==""||purge==nil||uploads==nil{return ErrInvalid}
 if err:=ctx.Err();err!=nil{return err}
 if err:=purge(ctx,key);err!=nil{return err}
 scan,cancel:=context.WithCancel(ctx);defer cancel()
 rows:=uploads(scan,key);if rows==nil{return ErrUnavailable}
 return awaitExactMultipartAbsent(ctx,key,rows)
}
