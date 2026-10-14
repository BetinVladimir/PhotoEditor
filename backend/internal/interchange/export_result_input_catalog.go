package interchange
import (
 "context"
 "errors"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)
// Root must be the exact canonical succeeded result, independently read/checked
// by ExportResultReader. Typed registered membership is read scope only, never
// a substitute for full schema/source/runtime/native qualification.
type SQLExportResultInputCatalog struct{Export SQLExportOutputCatalog;Root v2input.Ref}
func(s SQLExportResultInputCatalog)LookupExportDocument(ctx context.Context,entry v2input.Entry)(objectstore.Object,error){
 if err:=ctx.Err();err!=nil{return objectstore.Object{},err}
 config:=s.Export
 if config.DB==nil||len(config.Bucket)<3||len(config.Bucket)>63||!validImportRef(s.Root)||!validImportRef(entry.Ref)||s.Root.ArtifactID==entry.Ref.ArtifactID{return objectstore.Object{},v2input.ErrInvalid}
 intent,err:=FreezeCreateIntent(config.Tenant,config.Who,config.Request,config.Key);if err!=nil{return objectstore.Object{},err}
 if _,_,err:=DecodeFrozenExportDocuments(ctx,intent,config.Frozen.Body());err!=nil{return objectstore.Object{},err}
 var object objectstore.Object
 err=tenant.WithTenant(ctx,config.DB,config.Who,config.Tenant,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var bucket string
  err:=tx.QueryRow(ctx,`SELECT bucket,object_key,object_version,sha256,bytes,media_type FROM photo.v30_interchange_export_result_input($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,config.Tenant,config.Who.UserID,config.Who.SessionID,config.Key,intent.RequestSHA,s.Root.ArtifactID,s.Root.SHA256,entry.Ref.ArtifactID,entry.Ref.SHA256,entry.Contract).Scan(&bucket,&object.Key,&object.VersionID,&object.SHA256,&object.Bytes,&object.ContentType)
  if err!=nil{return err}
  if bucket!=config.Bucket||object.Key==""||object.VersionID==""||object.VersionID=="null"||object.SHA256!=entry.Ref.SHA256||object.Bytes<1||object.Bytes>128<<30{return objectstore.ErrIntegrity};return nil
 })
 if errors.Is(err,pgx.ErrNoRows){return objectstore.Object{},artifactjson.ErrStale};if err!=nil{return objectstore.Object{},err};return object,ctx.Err()
}
// Installation keeps JSON semantics and binary qualification mandatory.
func NewSQLExportResultResolver(ctx context.Context,catalog SQLExportResultInputCatalog,objects ImportDocumentObjects,validator ExportTypedDocumentValidator,binaries ExportBinaryQualifier,maxBytes int64)(ExportNodeResolverRouter,error){
 if err:=ctx.Err();err!=nil{return ExportNodeResolverRouter{},err}
 if catalog.Export.DB==nil||!validImportRef(catalog.Root)||objects==nil||validator==nil||binaries==nil||maxBytes<1||maxBytes>128<<30{return ExportNodeResolverRouter{},v2input.ErrInvalid}
 intent,err:=FreezeCreateIntent(catalog.Export.Tenant,catalog.Export.Who,catalog.Export.Request,catalog.Export.Key);if err!=nil{return ExportNodeResolverRouter{},err}
 if _,_,err:=DecodeFrozenExportDocuments(ctx,intent,catalog.Export.Frozen.Body());err!=nil{return ExportNodeResolverRouter{},err}
 return ExportNodeResolverRouter{JSON:ExportJSONNodeResolver{Catalog:catalog,Objects:objects,Validator:validator},Binary:ExportBinaryNodeResolver{Catalog:catalog,Objects:objects,Qualifier:binaries,MaxBytes:maxBytes}},nil
}
