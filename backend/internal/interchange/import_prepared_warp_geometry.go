package interchange
import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/v2input"
)
type SQLPreparedImportWarpGeometry struct{Catalog SQLPreparedImportCatalog}
func preparedImportWarpGeometry(scope ImportPreparedScope,entry v2input.Entry,raw []byte)(ExportArrayGeometry,error){
 var zero ExportArrayGeometry
 if entry.Contract!="DenseWarpField"||len(raw)<2||len(raw)>8192{return zero,v2input.ErrInvalid}
 var occurrence ImportWarpOccurrence
 if err:=decodeImportDocument(raw,&occurrence);err!=nil{return zero,err}
 scope.Contract="DenseWarpField";scope.Warp=&occurrence
 if err:=validateImportWarpOccurrence(scope,entry.Ref);err!=nil{return zero,err}
 canvas,err:=canonical.Hash(occurrence.To);if err!=nil{return zero,err}
 geometry:=ExportArrayGeometry{Width:int(occurrence.To.Width),Height:int(occurrence.To.Height),CanvasSHA:canvas,DType:"float32"}
 if _,err:=exportArrayMaximumBytes(entry,geometry);err!=nil{return zero,err}
 return geometry,nil
}
func(s SQLPreparedImportWarpGeometry)ResolveExportArrayGeometry(ctx context.Context,entry v2input.Entry)(ExportArrayGeometry,error){
 var zero ExportArrayGeometry
 if err:=ctx.Err();err!=nil{return zero,err}
 catalog:=s.Catalog;store:=catalog.Store;scope:=catalog.Scope
 if store.DB==nil||entry.Contract!="DenseWarpField"||!validImportRef(entry.Ref){return zero,v2input.ErrInvalid}
 before,err:=catalog.LookupImportDocument(ctx,entry);if err!=nil{return zero,err}
 parsed:=append([]byte(nil),store.Parsed...)
 binding,err:=canonical.Marshal(scope.Binding);if err!=nil{return zero,err}
 var raw []byte
 err=database.TenantTransaction(ctx,store.DB,scope.Tenant,func(tx pgx.Tx)error{
  if err:=store.checkPreparedRun(ctx,tx);err!=nil{return err}
  return tx.QueryRow(ctx,`SELECT photo.v30_interchange_prepared_warp_geometry($1,$2,$3,$4,$5::jsonb,$6,$7)`,scope.Tenant,scope.Workflow,string(parsed),scope.Entry,string(binding),entry.Ref.ArtifactID,entry.Ref.SHA256).Scan(&raw)
 });if err!=nil{return zero,err}
 raw,err=canonical.Bytes(raw);if err!=nil{return zero,err}
 geometry,err:=preparedImportWarpGeometry(scope,entry,raw);if err!=nil{return zero,err}
 after,err:=catalog.LookupImportDocument(ctx,entry);if err!=nil{return zero,err}
 if before!=after{return zero,v2input.ErrInvalid}
 return geometry,ctx.Err()
}


func NewSQLPreparedImportWarpResolver(ctx context.Context,catalog SQLPreparedImportCatalog,objects ImportDocumentObjects,scratch *ExportArchiveScratch,probe ExportNativeProbe,runtime string,maxBytes int64)(ImportPreparedBinaryNodeResolver,error){
 var zero ImportPreparedBinaryNodeResolver
 if err:=ctx.Err();err!=nil{return zero,err}
 if catalog.Store.DB==nil||objects==nil||scratch==nil||probe==nil||!validImportRefForSHA(runtime)||maxBytes<1||maxBytes>128<<30{return zero,v2input.ErrInvalid}
 catalog.Store.Parsed=append([]byte(nil),catalog.Store.Parsed...)
 if catalog.Scope.Warp!=nil{owned:=*catalog.Scope.Warp;catalog.Scope.Warp=&owned}
 qualifier:=NativeExportArrayQualifier{Objects:objects,Scratch:scratch,Probe:probe,RuntimeSHA:runtime,Geometry:SQLPreparedImportWarpGeometry{Catalog:catalog}}
 return ImportPreparedBinaryNodeResolver{Catalog:catalog,Objects:objects,Qualifier:qualifier,MaxBytes:maxBytes},nil
}
