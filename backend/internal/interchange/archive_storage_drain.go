package interchange
import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type ArchiveMultipartDrain interface{DrainInterchangeMultipart(context.Context,uuid.UUID,uuid.UUID,string)error}
type ArchiveGenerationMultipartDrain interface{DrainInterchangeGenerationMultipart(context.Context,uuid.UUID,uuid.UUID,uuid.UUID,string)error}
type SQLArchiveStorageDrain struct{DB artifactjson.RowQuerier;Objects ArchiveMultipartDrain;Bucket string}
func(s SQLArchiveStorageDrain)AssertArchiveDrained(ctx context.Context,tid,upload uuid.UUID,bucket,key string)error{
 if err:=ctx.Err();err!=nil{return err}
 generation,err:=archiveStorageGeneration(tid,upload,key)
 if err!=nil||s.DB==nil||s.Objects==nil||bucket!=s.Bucket||len(bucket)<3||len(bucket)>63{return v2input.ErrInvalid}
 read:=func()(string,error){
  var multipart *string
  if err:=s.DB.QueryRow(ctx,`SELECT photo.v30_archive_drain_multipart($1,$2,$3,$4)`,tid,upload,bucket,key).Scan(&multipart);err!=nil{return "",err}
  if multipart==nil{return "",ErrArchiveCleanupLeaseLost}
  if *multipart!=""&&!objectstore.ValidInterchangeMultipartID(*multipart){return "",objectstore.ErrIntegrity}
  return *multipart,ctx.Err()
 }
 multipart,err:=read();if err!=nil{return err}
 if generation==uuid.Nil{
  if err:=s.Objects.DrainInterchangeMultipart(ctx,tid,upload,multipart);err!=nil{return err}
 }else{
  scoped,ok:=s.Objects.(ArchiveGenerationMultipartDrain);if !ok||multipart==""{return v2input.ErrInvalid}
  if err:=scoped.DrainInterchangeGenerationMultipart(ctx,tid,upload,generation,multipart);err!=nil{return err}
 }
 current,err:=read();if err!=nil{return err};if current!=multipart{return objectstore.ErrIntegrity};return ctx.Err()
}
var _ ArchiveStorageDrain=SQLArchiveStorageDrain{}
