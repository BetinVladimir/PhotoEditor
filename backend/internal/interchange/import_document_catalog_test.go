package interchange
import (
 "context"
 "testing"
 "photoeditor/backend/internal/v2input"
)
func TestImportDocumentCatalogRejectsIncompleteScopeBeforeSQL(t *testing.T){
 if _,err:=(SQLImportDocumentCatalog{}).LookupImportDocument(context.Background(),v2input.Entry{});err==nil{t.Fatal("missing catalog scope accepted")}
}
func TestImportDocumentCatalogHonorsCancellationBeforeScopeOrSQL(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=(SQLImportDocumentCatalog{}).LookupImportDocument(ctx,v2input.Entry{});err!=context.Canceled{t.Fatal("cancelled catalog lookup",err)}
}
