package interchange
import (
 "context"
 "photoeditor/backend/internal/v2input"
)
// Requires independently installed schema/source semantics and native binary
// qualification. This constructor does not imply runtime qualification passed.
func NewSQLExportResolver(ctx context.Context,catalog SQLExportDocumentCatalog,objects ImportDocumentObjects,dependencies ExportTypedDocumentValidator,binaries ExportBinaryQualifier,maxBytes int64)(ExportNodeResolverRouter,error){
 if catalog.DB==nil||objects==nil||dependencies==nil||binaries==nil||maxBytes<1||maxBytes>128<<30{return ExportNodeResolverRouter{},v2input.ErrInvalid}
 intent,err:=FreezeCreateIntent(catalog.Tenant,catalog.Who,catalog.Request,catalog.Key);if err!=nil{return ExportNodeResolverRouter{},err}
 validator,err:=NewExportScopedDocumentValidator(ctx,intent,catalog.Frozen,dependencies);if err!=nil{return ExportNodeResolverRouter{},err}
 return ExportNodeResolverRouter{JSON:ExportJSONNodeResolver{Catalog:catalog,Objects:objects,Validator:validator},Binary:ExportBinaryNodeResolver{Catalog:catalog,Objects:objects,Qualifier:binaries,MaxBytes:maxBytes}},nil
}
