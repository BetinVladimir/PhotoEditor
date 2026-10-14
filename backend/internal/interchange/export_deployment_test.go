package interchange
import (
 "context"
 "strings"
 "testing"
 "photoeditor/backend/internal/canonical"
)
func nativeExportDeploymentFixture(t *testing.T)nativeExportDeploymentWire{
 t.Helper();sha:=strings.Repeat("a",64)
 report,err:=canonical.Marshal(ExportResourceMeasurements{Schema:"interchange-export-resource-measurements/v1",Runtime:sha,Contracts:sha,Hardware:sha,Measurements:[]ExportResourceMeasurement{{FrozenSHA:sha,InputDigest:sha,RAM:2<<30,Scratch:2<<30,Output:1<<30,InputJSON:128<<20}}});if err!=nil{t.Fatal(err)}
 return nativeExportDeploymentWire{Schema:"interchange-export-deployment/v1",Python:"/qualified/python",WorkerRoot:"/qualified/workers",ScratchRoot:"/qualified/scratch",ScratchBytes:8<<30,TimeoutSeconds:60,ImageRuntime:sha,ArrayRuntime:sha,Runtime:sha,Contracts:sha,Hardware:sha,MeasurementSHA:rawSHA(report),Measurements:report,MaxInputBytes:8<<30,ArchiveBytes:8<<30,ExpandedBytes:16<<30,Files:30000}
}
func TestNativeExportDeploymentPinnedClosedAndOwned(t *testing.T){
 w:=nativeExportDeploymentFixture(t);raw,_:=canonical.Marshal(w);saved:=string(raw)
 d,err:=DecodeNativeExportDeployment(raw,rawSHA(raw));if err!=nil{t.Fatal(err)};raw[0]='!';if d.raw!=saved{t.Fatal("deployment aliases input")}
 for _,alter:=range []func(*nativeExportDeploymentWire){func(w *nativeExportDeploymentWire){w.Python="relative"},func(w *nativeExportDeploymentWire){w.TimeoutSeconds=601},func(w *nativeExportDeploymentWire){w.Hardware=strings.Repeat("b",64)},func(w *nativeExportDeploymentWire){w.MeasurementSHA=strings.Repeat("b",64)},func(w *nativeExportDeploymentWire){w.Files=30001},func(w *nativeExportDeploymentWire){w.ImageRuntime="bad"}}{bad:=w;alter(&bad);body,_:=canonical.Marshal(bad);if _,err:=DecodeNativeExportDeployment(body,rawSHA(body));err==nil{t.Fatal("invalid deployment accepted")}}
 if _,err:=DecodeNativeExportDeployment([]byte(saved),strings.Repeat("b",64));err==nil{t.Fatal("foreign digest accepted")}
 extra:=append([]byte(`{"unknown":true,`),[]byte(saved)[1:]...);if _,err:=DecodeNativeExportDeployment(extra,rawSHA(extra));err==nil{t.Fatal("unknown configuration accepted")}
 if _,err:=d.Build(context.Background(),SQLExportExecutionInstaller{});err==nil{t.Fatal("missing ports touched deployment")}
 ctx,cancel:=context.WithCancel(context.Background());cancel();if _,err:=d.Build(ctx,SQLExportExecutionInstaller{});err!=context.Canceled{t.Fatal(err)}
}
