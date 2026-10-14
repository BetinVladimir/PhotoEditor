package interchange
import (
 "context"
 "testing"
)
func TestBodyOriginInstallationRequiresScope(t *testing.T){
 s:=SQLExportBodyOriginInstallation{}
 q,err:=s.BuildExportBodyOriginValidator(context.Background(),CreateIntent{},FrozenExportDocuments{},SQLExportDocumentCatalog{},nil,nil,nil,nil,nil,nil)
 if err==nil||q!=nil{t.Fatal("incomplete body installation accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 q,err=s.BuildExportBodyOriginValidator(ctx,CreateIntent{},FrozenExportDocuments{},SQLExportDocumentCatalog{},nil,nil,nil,nil,nil,nil)
 if err!=context.Canceled||q!=nil{t.Fatal("cancellation lost",err)}
}
