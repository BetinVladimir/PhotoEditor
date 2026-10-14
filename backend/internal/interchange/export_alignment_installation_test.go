package interchange
import (
 "context"
 "testing"
)
func TestAlignmentInstallationRejectsIncompleteScope(t *testing.T){
 installation:=SQLExportAlignmentInstallation{}
 q,err:=installation.BuildExportAlignmentQualifier(context.Background(),CreateIntent{},FrozenExportDocuments{},nil,nil,nil,nil,nil)
 if err==nil||q!=nil{t.Fatal("incomplete registration authority installed")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 q,err=installation.BuildExportAlignmentQualifier(ctx,CreateIntent{},FrozenExportDocuments{},nil,nil,nil,nil,nil)
 if err!=context.Canceled||q!=nil{t.Fatal("cancelled registration authority installed",err)}
}
