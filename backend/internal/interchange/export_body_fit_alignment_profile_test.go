package interchange
import (
 "context"
 "strings"
 "testing"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)
func TestAlignmentProfileRequiresExactOperatorLocks(t *testing.T){
 for _,mode:=range []string{"valid","operation","version","cuda","license","model","runtime","device","dataset","uppercase"}{t.Run(mode,func(t *testing.T){
  expected:=v2qualification.Expected{Operation:"pair.register",Version:"1",Backend:"cpu",LicenseCleared:true,ModelLockSHA:strings.Repeat("a",64),RuntimeLockSHA:strings.Repeat("b",64),DeviceProfileSHA:strings.Repeat("c",64),DatasetSHA:strings.Repeat("d",64)}
  switch mode{case "operation":expected.Operation="pair.fit.body";case "version":expected.Version="2";case "cuda":expected.Backend="cuda";case "license":expected.LicenseCleared=false;case "model":expected.ModelLockSHA="";case "runtime":expected.RuntimeLockSHA="short";case "device":expected.DeviceProfileSHA=strings.Repeat("g",64);case "dataset":expected.DatasetSHA="";case "uppercase":expected.ModelLockSHA=strings.Repeat("A",64)}
  if validExportAlignmentProfile(expected)!=(mode=="valid"){t.Fatal("invalid registration profile accepted")}
 })}
}
func TestAlignmentProfileSQLRequiresDatabaseAndLiveContext(t *testing.T){
 source:=SQLExportDocumentCatalog{}
 if err:=source.MatchExportBodyFitAlignmentProfile(context.Background(),ExportBodyDependencyOrigin{},v2input.Entry{},nil,ExportClosureNode{},v2input.Entry{},v2qualification.Expected{});err!=v2input.ErrInvalid{t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=source.MatchExportBodyFitAlignmentProfile(ctx,ExportBodyDependencyOrigin{},v2input.Entry{},nil,ExportClosureNode{},v2input.Entry{},v2qualification.Expected{});err!=context.Canceled{t.Fatal(err)}
}
