package interchange
import (
 "bytes"
 "context"
 "io"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
// LookupImportDocument must authorize exact tenant/project/workflow provenance,
// current ACL and ready immutable object metadata in a short SQL transaction.
type ImportDocumentCatalog interface{LookupImportDocument(context.Context,v2input.Entry)(objectstore.Object,error)}
type ImportDocumentObjects interface{Open(context.Context,objectstore.Object)(io.ReadCloser,error)}
type ImportTypedDocumentValidator interface{ImportDocumentChildren(context.Context,v2input.Entry,[]byte)([]v2input.Entry,error)}
type ImportJSONNodeResolver struct{Catalog ImportDocumentCatalog;Objects ImportDocumentObjects;Validator ImportTypedDocumentValidator}
// ResolveImportNode performs object IO outside catalog transactions and checks
// the current object again after typed validation. Binary leaves require their
// own physical/typed qualifier; this JSON resolver never accepts them as JSON.
func(r ImportJSONNodeResolver)ResolveImportNode(ctx context.Context,entry v2input.Entry)(ImportClosureNode,error){
 if err:=ctx.Err();err!=nil{return ImportClosureNode{},err}
 if r.Catalog==nil||r.Objects==nil||r.Validator==nil{return ImportClosureNode{},v2input.ErrInvalid}
 object,err:=r.Catalog.LookupImportDocument(ctx,entry);if err!=nil{return ImportClosureNode{},err}
 if object.Key==""||object.VersionID==""||object.VersionID=="null"||object.ContentType!="application/json"||object.SHA256!=entry.Ref.SHA256||object.Bytes<2||object.Bytes>8<<20{return ImportClosureNode{},v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return ImportClosureNode{},err}
 reader,err:=r.Objects.Open(ctx,object);if err!=nil{if reader!=nil{_ = reader.Close()};return ImportClosureNode{},err};if reader==nil{return ImportClosureNode{},v2input.ErrInvalid}
 raw,readErr:=io.ReadAll(io.LimitReader(exportContextReader{ctx:ctx,reader:reader},object.Bytes+1));closeErr:=reader.Close()
 if err:=ctx.Err();err!=nil{return ImportClosureNode{},err}
 if readErr!=nil{return ImportClosureNode{},readErr};if closeErr!=nil{return ImportClosureNode{},closeErr}
 if int64(len(raw))!=object.Bytes||rawSHA(raw)!=entry.Ref.SHA256{return ImportClosureNode{},v2input.ErrInvalid}
 normalized,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(normalized,raw){return ImportClosureNode{},v2input.ErrInvalid}
 children,err:=r.Validator.ImportDocumentChildren(ctx,entry,append([]byte(nil),raw...));if err!=nil{return ImportClosureNode{},err}
 if err:=ctx.Err();err!=nil{return ImportClosureNode{},err}
 current,err:=r.Catalog.LookupImportDocument(ctx,entry);if err!=nil{return ImportClosureNode{},err}
 if current!=object{return ImportClosureNode{},v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return ImportClosureNode{},err}
 return ImportClosureNode{Entry:entry,Body:raw,Children:append([]v2input.Entry(nil),children...)},nil
}
