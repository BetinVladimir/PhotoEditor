package interchange
import (
 "context"
 "photoeditor/backend/internal/v2input"
)
// The qualifier must independently resolve prepared occurrence reports and
// label-map witnesses. SQL readability by itself is not physical qualification.
type ImportPreparedPNGNodeResolver struct{Catalog ImportDocumentCatalog;Qualifier ExportBinaryQualifier}
func(r ImportPreparedPNGNodeResolver)ResolveImportNode(ctx context.Context,entry v2input.Entry)(ImportClosureNode,error){
 zero:=ImportClosureNode{}
 if err:=ctx.Err();err!=nil{return zero,err}
 if entry.Contract!="DefectLabelPNG"||!validImportRef(entry.Ref)||r.Catalog==nil||r.Qualifier==nil{return zero,v2input.ErrInvalid}
 object,err:=r.Catalog.LookupImportDocument(ctx,entry);if err!=nil{return zero,err}
 if object.ContentType!="image/png"||object.Bytes<8||object.Bytes>512<<20||object.SHA256!=entry.Ref.SHA256{return zero,v2input.ErrInvalid}
 if err:=r.Qualifier.QualifyExportBinary(ctx,entry,object);err!=nil{return zero,err}
 current,err:=r.Catalog.LookupImportDocument(ctx,entry);if err!=nil{return zero,err};if current!=object{return zero,v2input.ErrInvalid}
 return ImportClosureNode{Entry:entry,Children:[]v2input.Entry{}},ctx.Err()
}
