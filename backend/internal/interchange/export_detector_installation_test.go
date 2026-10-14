package interchange
import (
 "context"
 "testing"
)
func TestDetectorInstallationRequiresCompleteRequestScope(t *testing.T){
 installation:=SQLExportDetectorInstallation{}
 authority,err:=installation.BuildExportDetectorQualifier(context.Background(),CreateIntent{},FrozenExportDocuments{},nil,nil,nil,nil,nil)
 if err==nil||authority!=nil{t.Fatal("incomplete detector authority installed")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 authority,err=installation.BuildExportDetectorQualifier(ctx,CreateIntent{},FrozenExportDocuments{},nil,nil,nil,nil,nil)
 if err!=context.Canceled||authority!=nil{t.Fatal("cancelled detector authority installed",err)}
}
