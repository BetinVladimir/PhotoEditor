package provenance

import (
 "context"
 "encoding/json"
 "os"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/v2optical"
 "photoeditor/backend/internal/v2qualification"
)

func TestSigningNegativePolicyRegistryOwnsExactHistoricalLocks(t *testing.T){
 raw,err:=os.ReadFile("../v2optical/testdata/negative-qualification.json");if err!=nil{t.Fatal(err)}
 var fixture struct{Pin v2optical.NegativeQualificationPin;Locks v2qualification.DeploymentLocks}
 if json.Unmarshal(raw,&fixture)!=nil{t.Fatal("bad fixture")}
 entry:=InstalledSigningNegativePolicy{PolicyID:fixture.Pin.PolicyID,PolicySHA:fixture.Pin.PolicySHA,Locks:fixture.Locks}
 registry,err:=NewSigningNegativePolicyRegistry([]InstalledSigningNegativePolicy{entry});if err!=nil{t.Fatal(err)}
 entry.Locks.RequiredChecks[0]="caller-mutated"
 locks,err:=registry.Resolve(context.Background(),fixture.Pin);if err!=nil{t.Fatal(err)}
 locks.RequiredChecks[0]="return-mutated"
 if _,err:=registry.Resolve(context.Background(),fixture.Pin);err!=nil{t.Fatal("registry aliased caller",err)}
 for _,mode:=range []string{"policy","sha","model","runtime","issuer","calibration"}{
  pin:=fixture.Pin
  switch mode{
  case "policy":pin.PolicyID=uuid.New()
  case "sha":pin.PolicySHA=pin.ModelLockSHA
  case "model":pin.ModelLockSHA=pin.RuntimeLockSHA
  case "runtime":pin.RuntimeLockSHA=pin.ModelLockSHA
  case "issuer":pin.IssuerID=uuid.New()
  case "calibration":pin.Calibration.SHA256=pin.ModelLockSHA
  }
  if _,err:=registry.Resolve(context.Background(),pin);err==nil{t.Fatal("wrong historical identity accepted",mode)}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=registry.Resolve(ctx,fixture.Pin);err!=context.Canceled{t.Fatal(err)}
}

func TestSigningNegativePolicyRegistryRejectsDuplicateOrUnboundInstallation(t *testing.T){
 raw,err:=os.ReadFile("../v2optical/testdata/negative-qualification.json");if err!=nil{t.Fatal(err)}
 var fixture struct{Pin v2optical.NegativeQualificationPin;Locks v2qualification.DeploymentLocks}
 if json.Unmarshal(raw,&fixture)!=nil{t.Fatal("bad fixture")}
 entry:=InstalledSigningNegativePolicy{PolicyID:fixture.Pin.PolicyID,PolicySHA:fixture.Pin.PolicySHA,Locks:fixture.Locks}
 if _,err:=NewSigningNegativePolicyRegistry([]InstalledSigningNegativePolicy{entry,entry});err==nil{t.Fatal("duplicate policy accepted")}
 entry.PolicySHA=entry.Locks.RuntimeLockSHA
 if _,err:=NewSigningNegativePolicyRegistry([]InstalledSigningNegativePolicy{entry});err==nil{t.Fatal("wrong policy digest accepted")}
 if _,err:=NewSigningNegativePolicyRegistry(nil);err==nil{t.Fatal("empty installation accepted")}
}
