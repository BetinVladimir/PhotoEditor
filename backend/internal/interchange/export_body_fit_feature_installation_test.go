package interchange
import (
 "context"
 "testing"
)
func TestPairedFeatureInstallationRequiresCompleteRequestScope(t *testing.T){
 installation:=SQLExportBodyFitFeatureInstallation{}
 authority,err:=installation.BuildExportBodyFitFeatureQualifier(context.Background(),CreateIntent{},FrozenExportDocuments{},nil,nil,nil,nil,nil)
 if err==nil||authority!=nil{t.Fatal("incomplete paired feature authority installed")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 authority,err=installation.BuildExportBodyFitFeatureQualifier(ctx,CreateIntent{},FrozenExportDocuments{},nil,nil,nil,nil,nil)
 if err!=context.Canceled||authority!=nil{t.Fatal("cancelled paired feature authority installed",err)}
}
