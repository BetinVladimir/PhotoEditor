package provenance

import (
 "encoding/json"
 "os"
 "testing"
 "photoeditor/backend/internal/v2optical"
 "photoeditor/backend/internal/v2qualification"
)

func TestRenderSigningInstallationPreservesConfiguredNegativeAncestry(t *testing.T){
 raw,err:=os.ReadFile("../v2optical/testdata/negative-qualification.json");if err!=nil{t.Fatal(err)}
 var fixture struct{Pin v2optical.NegativeQualificationPin;Locks v2qualification.DeploymentLocks}
 if err=json.Unmarshal(raw,&fixture);err!=nil{t.Fatal(err)}
 entry:=InstalledSigningNegativePolicy{PolicyID:fixture.Pin.PolicyID,PolicySHA:fixture.Pin.PolicySHA,Locks:fixture.Locks}
 testSigningServicePinnedDependencies(t,func(config PresentationSigningServiceInstallation)(*SQLSignExportService,error){
  config.NegativePolicies=[]InstalledSigningNegativePolicy{entry}
  service,err:=NewSQLRenderPresentationSigningService(config)
  if err!=nil{return nil,err}
  source:=service.Preparation.Source.(BoundSigningExportSource)
  lineage:=source.Lineages.(CompiledExportLineageSource)
  execution:=lineage.Execution.(BoundSigningExecutionSource)
  router,ok:=execution.Claims.(NegativeSigningTaskRouter)
  if !ok||router.Negative==nil||router.Existing==nil{t.Fatal("negative ancestry was dropped")}
  if _,ok:=router.Existing.(RenderPresentationSigningTaskResolver);!ok{t.Fatal("render/presentation ancestry was dropped")}
  return service,nil
 },false)
}

func TestExplicitEmptyNegativeSigningInstallationRefusesFallback(t *testing.T){
 if _,err:=NewSQLRenderPresentationSigningService(PresentationSigningServiceInstallation{NegativePolicies:[]InstalledSigningNegativePolicy{}});err==nil{t.Fatal("explicit empty negative policies fell back")}
}
