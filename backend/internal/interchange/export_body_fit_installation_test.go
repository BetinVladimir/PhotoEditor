package interchange
import (
 "context"
 "testing"
)
func TestBodyFitInstallationRequiresCompleteScope(t *testing.T){
 s:=SQLExportBodyFitInstallation{}
 q,err:=s.BuildExportBodyFitQualifier(context.Background(),CreateIntent{},FrozenExportDocuments{},SQLExportDocumentCatalog{},nil,nil,nil,nil,nil)
 if err==nil||q!=nil{t.Fatal("incomplete installation accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 q,err=s.BuildExportBodyFitQualifier(ctx,CreateIntent{},FrozenExportDocuments{},SQLExportDocumentCatalog{},nil,nil,nil,nil,nil)
 if err!=context.Canceled||q!=nil{t.Fatal("cancellation lost",err)}
}
