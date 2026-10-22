package interchange

import (
 "context"
 "testing"
 "photoeditor/backend/internal/canonical"
)
func TestMeasuredExportResourcesBindExactWorkloadAndInstallation(t *testing.T){
 _,_,intent,frozen,resolver:=exportExecutionFixture(t)
 ctx:=context.Background()
 _,compiled,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());if err!=nil{t.Fatal(err)}
 inventory,err:=ResolveExportInputInventory(ctx,compiled,resolver);if err!=nil{t.Fatal(err)}
 runtime,contracts,hardware:=rawSHA([]byte("runtime")),rawSHA([]byte("contracts")),rawSHA([]byte("hardware"))
 measurement:=ExportResourceMeasurement{FrozenSHA:rawSHA(frozen.Body()),InputDigest:inventory.digest,RAM:2<<30,Scratch:2<<30,Output:1<<30,InputJSON:128<<20}
 report:=ExportResourceMeasurements{Schema:"interchange-export-resource-measurements/v1",Runtime:runtime,Contracts:contracts,Hardware:hardware,Measurements:[]ExportResourceMeasurement{measurement}}
 raw,err:=canonical.Marshal(report);if err!=nil{t.Fatal(err)}
 qualifier,err:=NewMeasuredExportResourceQualifier(raw,rawSHA(raw),runtime,contracts,hardware);if err!=nil{t.Fatal(err)}
 resources,err:=qualifier.QualifyExportResources(ctx,intent,frozen,inventory);if err!=nil||resources.RAMBytes!=measurement.RAM||resources.ContractsSHA!=contracts{t.Fatal("exact workload rejected",err)}
 selected,err:=qualifier.SelectExportResources(ctx,intent,frozen)
 if err!=nil||selected!=resources{t.Fatal("selected profile differs from qualification",err)}
 ambiguous:=report;different:=measurement;different.InputDigest=rawSHA([]byte("another inventory"));different.RAM++
 ambiguous.Measurements=append(append([]ExportResourceMeasurement(nil),report.Measurements...),different)
 ambiguousRaw,err:=canonical.Marshal(ambiguous);if err!=nil{t.Fatal(err)}
 ambiguousQualifier,err:=NewMeasuredExportResourceQualifier(ambiguousRaw,rawSHA(ambiguousRaw),runtime,contracts,hardware);if err!=nil{t.Fatal(err)}
 if _,err:=ambiguousQualifier.SelectExportResources(ctx,intent,frozen);err==nil{t.Fatal("ambiguous resource envelope selected")}
 raw[0]='x'
 if _,err:=qualifier.QualifyExportResources(ctx,intent,frozen,inventory);err!=nil{t.Fatal("qualifier retained input bytes",err)}
 changed:=inventory;changed.entries=inventory.Entries();changed.entries[0].Contract="substituted"
 if _,err:=qualifier.QualifyExportResources(ctx,intent,frozen,changed);err==nil{t.Fatal("digest trusted over actual inventory")}
 changed=inventory;changed.digest=rawSHA([]byte("new inventory"))
 if _,err:=qualifier.QualifyExportResources(ctx,intent,frozen,changed);err==nil{t.Fatal("unmeasured workload accepted")}
 cancelled,cancel:=context.WithCancel(ctx);cancel()
 if _,err:=qualifier.QualifyExportResources(cancelled,intent,frozen,inventory);err!=context.Canceled{t.Fatal("cancellation lost",err)}
 for _,change:=range []string{"pin","runtime","contracts","hardware","duplicate","unsafe","empty","schema"}{
  altered:=report;altered.Measurements=append([]ExportResourceMeasurement(nil),report.Measurements...)
  switch change{case "duplicate":altered.Measurements=append(altered.Measurements,measurement)
  case "unsafe":altered.Measurements[0].RAM=9007199254740992
  case "empty":altered.Measurements=nil
  case "schema":altered.Schema="unknown"}
  body,err:=canonical.Marshal(altered);if err!=nil{if change=="unsafe"{continue};t.Fatal(err)}
  pin,r,c,h:=rawSHA(body),runtime,contracts,hardware
  switch change{case "pin":pin=rawSHA([]byte("wrong pin"));case "runtime":r=rawSHA([]byte("wrong runtime"));case "contracts":c=rawSHA([]byte("wrong contracts"));case "hardware":h=rawSHA([]byte("wrong hardware"))}
  if _,err:=NewMeasuredExportResourceQualifier(body,pin,r,c,h);err==nil{t.Fatal("invalid measurement accepted",change)}
 }
}
