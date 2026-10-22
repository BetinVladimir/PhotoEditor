package interchange
import (
 "context"
 "testing"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type exportReaderCatalog struct{*importCatalogStub}
func(s exportReaderCatalog)LookupExportDocument(ctx context.Context,entry v2input.Entry)(objectstore.Object,error){return s.LookupImportDocument(ctx,entry)}
type exportReaderValidator struct{*importValidatorStub}
func(s exportReaderValidator)ExportDocumentChildren(ctx context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){return s.ImportDocumentChildren(ctx,entry,raw)}
func TestExportJSONReaderReusesPhysicalAndTypedChecks(t *testing.T){
 for _,mode:=range []string{"valid","hash","version","media","binary_contract","defect_png_contract","cancelled"}{t.Run(mode,func(t *testing.T){
  entry,catalog,objects,validator:=importReaderFixture()
  ctx:=context.Background()
  switch mode{case "hash":objects.body=[]byte(`{"schema_version":2}`);case "version":catalog.change=true;case "media":catalog.object.ContentType="image/tiff";case "binary_contract":entry.Contract="EncodedImage";case "defect_png_contract":entry.Contract="DefectLabelPNG";case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  node,err:=(ExportJSONNodeResolver{Catalog:exportReaderCatalog{catalog},Objects:objects,Validator:exportReaderValidator{validator}}).ResolveExportNode(ctx,entry)
  if mode=="valid"{
   if err!=nil||catalog.calls!=2||objects.opens!=1||validator.calls!=1||rawSHA(node.Body)!=entry.Ref.SHA256{t.Fatal("invalid physical/typed read",err)}
  }else if err==nil{t.Fatal("invalid export read accepted")}
  if mode=="binary_contract"||mode=="defect_png_contract"||mode=="cancelled"{if catalog.calls!=0||objects.opens!=0||validator.calls!=0{t.Fatal("invalid input reached IO")}}
 })}
}
