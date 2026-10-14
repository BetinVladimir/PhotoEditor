package objectstore

import (
 "context"
 "github.com/google/uuid"
 "github.com/minio/minio-go/v7"
)
// DiscoverInterchangeMultipart is read-only evidence for a fenced Begin writer.
// An empty listing is not proof that a timed-out Create cannot complete later.
// Caller must not close an unknown writer or issue a new Begin on this alone.
func(s *Store)DiscoverInterchangeMultipart(ctx context.Context,tenant,upload uuid.UUID)(string,error){
 key,err:=InterchangeUploadKey(tenant,upload);if err!=nil{return "",err}
 if s==nil||s.client==nil||s.bucket==""{return "",ErrInvalid}
 return discoverInterchangeMultipart(ctx,key,func(scan context.Context)<-chan minio.ObjectMultipartInfo{return s.client.ListIncompleteUploads(scan,s.bucket,key,true)})
}
func discoverInterchangeMultipart(ctx context.Context,key string,list func(context.Context)<-chan minio.ObjectMultipartInfo)(string,error){
 if !validInterchangeUploadKey(key)||list==nil{return "",ErrInvalid}
 return discoverExactMultipart(ctx,key,list)
}
func discoverExactMultipart(ctx context.Context,key string,list func(context.Context)<-chan minio.ObjectMultipartInfo)(string,error){
 if key==""||list==nil{return "",ErrInvalid}
 if err:=ctx.Err();err!=nil{return "",err}
 scan,cancel:=context.WithCancel(ctx);defer cancel()
 rows:=list(scan);if rows==nil{return "",ErrUnavailable}
 found:="";count:=0
 for {
  select {
  case <-ctx.Done():return "",ctx.Err()
  case row,ok:=<-rows:
   if !ok{return found,ctx.Err()}
   count++;if count>10000{return "",ErrUnavailable}
   if row.Err!=nil{return "",classify(row.Err)}
   if row.Key!=key{continue}
   if !ValidInterchangeMultipartID(row.UploadID){return "",ErrIntegrity}
   // Even repeated identical rows mean the listing is not a unique receipt.
   if found!=""{return "",ErrIntegrity}
   found=row.UploadID
  }
 }
}
