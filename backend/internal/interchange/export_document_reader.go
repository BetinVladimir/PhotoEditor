package interchange

import (
 "context"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

// The catalog must authorize current tenant/project/snapshot provenance and
// return verified immutable metadata; a matching hash alone is not authority.
type ExportDocumentCatalog interface{LookupExportDocument(context.Context,v2input.Entry)(objectstore.Object,error)}
type ExportTypedDocumentValidator interface{ExportDocumentChildren(context.Context,v2input.Entry,[]byte)([]v2input.Entry,error)}
type ExportJSONNodeResolver struct{Catalog ExportDocumentCatalog;Objects ImportDocumentObjects;Validator ExportTypedDocumentValidator}
type exportCatalogBridge struct{catalog ExportDocumentCatalog}
func(b exportCatalogBridge)LookupImportDocument(ctx context.Context,entry v2input.Entry)(objectstore.Object,error){return b.catalog.LookupExportDocument(ctx,entry)}
type exportValidatorBridge struct{validator ExportTypedDocumentValidator}
func(b exportValidatorBridge)ImportDocumentChildren(ctx context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){return b.validator.ExportDocumentChildren(ctx,entry,raw)}

// Reuse the bounded immutable JSON reader, including SHA/canonical checks and
// catalog recheck after typed validation. Typed children are mandatory and must
// come from schemas/source semantics, never from caller contract labels.
func(r ExportJSONNodeResolver)ResolveExportNode(ctx context.Context,entry v2input.Entry)(ExportClosureNode,error){
 if err:=ctx.Err();err!=nil{return ExportClosureNode{},err}
 if r.Catalog==nil||r.Objects==nil||r.Validator==nil||!validImportRef(entry.Ref){return ExportClosureNode{},v2input.ErrInvalid}
 switch entry.Contract{
 case "EncodedImage","StyleBackgroundAsset","WorkingPixels","WorkingAlpha","PixelArray","MaskPixels","MaskArray","DenseWarpField","DenseWarpArray","DefectLabelMap","DefectLabelPNG","AbsoluteLUTArray":return ExportClosureNode{},v2input.ErrInvalid
 }
 return (ImportJSONNodeResolver{Catalog:exportCatalogBridge{r.Catalog},Objects:r.Objects,Validator:exportValidatorBridge{r.Validator}}).ResolveImportNode(ctx,entry)
}
