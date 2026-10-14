package interchange
import (
 "bytes"
 "context"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type SQLImportOpaqueCatalog struct{DB *pgxpool.Pool;Tenant,Workflow uuid.UUID;Parsed []byte;Bucket string}
func(s SQLImportOpaqueCatalog)LookupImportDocument(ctx context.Context,entry v2input.Entry)(objectstore.Object,error){
 if err:=ctx.Err();err!=nil{return objectstore.Object{},err}
 if s.DB==nil||s.Tenant==uuid.Nil||s.Workflow==uuid.Nil||len(s.Bucket)<3||len(s.Bucket)>63||entry.Ref.ArtifactID==uuid.Nil||len(entry.Ref.SHA256)!=64||len(s.Parsed)<2||len(s.Parsed)>8<<20{return objectstore.Object{},v2input.ErrInvalid}
 if entry.Contract!="Artifact"{return objectstore.Object{},v2input.ErrInvalid}
 parsed:=append([]byte(nil),s.Parsed...);normalized,err:=canonical.Bytes(parsed);if err!=nil||!bytes.Equal(normalized,parsed){return objectstore.Object{},v2input.ErrInvalid}
 var object objectstore.Object
 err=database.TenantTransaction(ctx,s.DB,s.Tenant,func(tx pgx.Tx)error{
  var bucket string
  if err:=tx.QueryRow(ctx,`SELECT bucket,object_key,object_version,sha256,bytes,media_type FROM photo.v30_interchange_import_file($1,$2,$3,$4,$5)`,s.Tenant,s.Workflow,string(parsed),entry.Ref.ArtifactID,entry.Ref.SHA256).Scan(&bucket,&object.Key,&object.VersionID,&object.SHA256,&object.Bytes,&object.ContentType);err!=nil{return err}
  if bucket!=s.Bucket||object.SHA256!=entry.Ref.SHA256||object.Bytes<1||object.Bytes>128<<30||object.VersionID==""||object.VersionID=="null"||object.ContentType!="application/octet-stream"||object.Key==""{return objectstore.ErrIntegrity}
  return nil
 })
 if errors.Is(err,pgx.ErrNoRows){return objectstore.Object{},artifactjson.ErrStale}
 if err!=nil{return objectstore.Object{},err};return object,nil
}
