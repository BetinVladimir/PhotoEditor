package interchange
import (
 "testing"
 "photoeditor/backend/internal/v2qualification"
)
func TestDetectorProfileRequiresExactCurrentOperatorLocks(t *testing.T){
 for _,mode:=range []string{"cpu","mps","cuda","operation","version","license","model","runtime","device","dataset","backend"}{t.Run(mode,func(t *testing.T){
  e:=v2qualification.Expected{Operation:"face.detect",Version:"1",Backend:"mps",LicenseCleared:true,ModelLockSHA:rawSHA([]byte("model")),RuntimeLockSHA:rawSHA([]byte("runtime")),DeviceProfileSHA:rawSHA([]byte("device")),DatasetSHA:rawSHA([]byte("dataset"))}
  switch mode{case "cpu","mps","cuda":e.Backend=mode;case "operation":e.Operation="face.landmarks";case "version":e.Version="2";case "license":e.LicenseCleared=false;case "model":e.ModelLockSHA="";case "runtime":e.RuntimeLockSHA="foreign";case "device":e.DeviceProfileSHA="";case "dataset":e.DatasetSHA="";case "backend":e.Backend="metal"}
  valid:=mode=="cpu"||mode=="mps"||mode=="cuda";if validExportDetectorProfile(e)!=valid{t.Fatal("invalid detector policy accepted")}
 })}
}
