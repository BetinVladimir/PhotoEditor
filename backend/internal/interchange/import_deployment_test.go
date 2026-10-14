package interchange
import (
 "context"
 "testing"
 "photoeditor/backend/internal/canonical"
)
func TestNativeImportDeploymentPinsCompleteConfiguration(t *testing.T){
 for _,mode:=range []string{"valid","pin","relative_python","runtime","measurement","timeout","schema"}{t.Run(mode,func(t *testing.T){
  sha:=func(s string)string{return rawSHA([]byte(s))}
  report:=ImportResourceMeasurements{Schema:"interchange-import-resource-measurements/v1",Runtime:sha("runtime"),Contracts:sha("contracts"),Hardware:sha("hardware"),Measurements:[]ImportResourceMeasurement{{DocumentsSHA:sha("docs"),InputDigest:sha("closure"),RAM:1<<30,Scratch:8<<20,Output:32<<20}}}
  measurements,err:=canonical.Marshal(report);if err!=nil{t.Fatal(err)}
  wire:=nativeImportDeploymentWire{Schema:"interchange-import-deployment/v1",Python:"/operator/python",WorkerRoot:"/operator/workers",ScratchRoot:"/operator/scratch",ScratchBytes:1<<30,TimeoutSeconds:30,ArrayRuntime:sha("array"),Runtime:report.Runtime,Contracts:report.Contracts,Hardware:report.Hardware,MeasurementSHA:rawSHA(measurements),Measurements:measurements,MaxInputBytes:1<<30}
  switch mode{case "relative_python":wire.Python="python";case "runtime":wire.Runtime=sha("different");case "measurement":wire.MeasurementSHA=sha("different");case "timeout":wire.TimeoutSeconds=0;case "schema":wire.Schema="interchange-export-deployment/v1"}
  raw,err:=canonical.Marshal(wire);if err!=nil{t.Fatal(err)};pin:=rawSHA(raw);if mode=="pin"{pin=sha("different")}
  deployment,err:=DecodeNativeImportDeployment(raw,pin)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected deployment decision",err)}
  if mode!="valid"&&deployment.raw!=""{t.Fatal("partial deployment returned")}
 })}
}
func TestNativeImportDeploymentBuildRequiresDependenciesBeforeScratch(t *testing.T){
 deps,resources,err:=(NativeImportDeployment{}).Build(context.Background(),nil,nil,"")
 if err==nil||resources!=nil||deps.WarpScratch!=nil{t.Fatal("incomplete native deployment built")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 _,_,err=(NativeImportDeployment{}).Build(ctx,nil,nil,"")
 if err!=context.Canceled{t.Fatal("cancellation lost",err)}
}
