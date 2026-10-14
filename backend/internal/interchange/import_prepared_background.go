package interchange
import (
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
// Read the stored occurrence and MIME behind the existing full certificate
// authority gate. A caller cannot choose MIME or supply a replacement receipt.
type SQLPreparedImportBackgroundCatalog struct{Catalog SQLPreparedImportCatalog}
func(c SQLPreparedImportBackgroundCatalog)LookupImportDocument(ctx context.Context,entry v2input.Entry)(objectstore.Object,error){
 if err:=ctx.Err();err!=nil{return objectstore.Object{},err}
 catalog:=c.Catalog;store:=catalog.Store;scope:=catalog.Scope
 if entry.Contract!="StyleBackgroundAsset"||!validImportRef(entry.Ref){return objectstore.Object{},v2input.ErrInvalid}
 probe:=scope;probe.Contract="MaskRef";probe.Warp=nil;probe.Background=nil
 if _,_,err:=store.receipt(probe,scope.Binding.Source,2,"application/json");err!=nil{return objectstore.Object{},err}
 binding,err:=canonical.Marshal(scope.Binding);if err!=nil{return objectstore.Object{},err}
 parsed:=append([]byte(nil),store.Parsed...)
 var raw []byte
 err=database.TenantTransaction(ctx,store.DB,scope.Tenant,func(tx pgx.Tx)error{
  if err:=store.checkPreparedRun(ctx,tx);err!=nil{return err}
  return tx.QueryRow(ctx,`SELECT photo.v30_interchange_prepared_background_certificate($1,$2,$3,$4,$5::jsonb,$6,$7)`,scope.Tenant,scope.Workflow,string(parsed),scope.Entry,string(binding),entry.Ref.ArtifactID,entry.Ref.SHA256).Scan(&raw)
 });if err!=nil{return objectstore.Object{},err}
 raw,err=canonical.Bytes(raw);if err!=nil{return objectstore.Object{},err}
 occurrence,media,err:=decodePreparedImportBackgroundCertificate(scope,entry,raw);if err!=nil{return objectstore.Object{},err}
 catalog.Scope.Background=&occurrence;catalog.Scope.Warp=nil;catalog.BackgroundMedia=media
 return catalog.LookupImportDocument(ctx,entry)
}
func decodePreparedImportBackgroundCertificate(scope ImportPreparedScope,entry v2input.Entry,raw []byte)(ImportBackgroundSource,string,error){
 var receipt struct{Background ImportBackgroundSource `json:"background_occurrence"`;Media string `json:"media_type"`}
 if entry.Contract!="StyleBackgroundAsset"||len(raw)<2||len(raw)>16384{return ImportBackgroundSource{},"",v2input.ErrInvalid}
 if err:=decodeImportDocument(raw,&receipt);err!=nil{return ImportBackgroundSource{},"",err}
 scope.Contract=entry.Contract;scope.Background=&receipt.Background;scope.Warp=nil
 if err:=validateImportBackgroundOccurrence(scope,entry.Ref);err!=nil{return ImportBackgroundSource{},"",err}
 switch receipt.Media{case "image/jpeg","image/png","image/tiff":default:return ImportBackgroundSource{},"",v2input.ErrInvalid}
 return receipt.Background,receipt.Media,nil
}
type ImportPreparedBackgroundNodeResolver struct{Catalog ImportDocumentCatalog;Native NativeExportImageQualifier}
func(r ImportPreparedBackgroundNodeResolver)ResolveImportNode(ctx context.Context,entry v2input.Entry)(ImportClosureNode,error){
 if err:=ctx.Err();err!=nil{return ImportClosureNode{},err}
 if r.Catalog==nil||entry.Contract!="StyleBackgroundAsset"||!validImportRef(entry.Ref){return ImportClosureNode{},v2input.ErrInvalid}
 before,err:=r.Catalog.LookupImportDocument(ctx,entry);if err!=nil{return ImportClosureNode{},err}
 if err:=r.Native.QualifyExportBinary(ctx,v2input.Entry{Ref:entry.Ref,Contract:"EncodedImage"},before);err!=nil{return ImportClosureNode{},err}
 after,err:=r.Catalog.LookupImportDocument(ctx,entry);if err!=nil{return ImportClosureNode{},err}
 if before!=after{return ImportClosureNode{},v2input.ErrInvalid}
 return ImportClosureNode{Entry:entry},ctx.Err()
}
