package rawdevelopment

import (
 "crypto/sha256"
 "encoding/hex"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2qualification"
)

func TestCapabilityRequiresEveryAdvertisedRAWCheck(t *testing.T) {
 report,expected:=rawCapabilityFixture()
 digest:=func(s string)string{return strings.Repeat(s,64)}
 verify:=func()(*Capability,error){
  raw,err:=canonical.Marshal(report);if err!=nil{t.Fatal(err)}
  sum:=sha256.Sum256(raw)
  return VerifyCapability(raw,hex.EncodeToString(sum[:]),"rawpy-0.27.1/libraw-0.21.4",expected)
 }
 capability,err:=verify();if err!=nil{t.Fatal(err)}
 if len(expected.RequiredChecks)!=0{t.Fatal("caller policy mutated")}
 policy:=&Policy{1,"dcb","auto",2,"blend","rawpy-0.27.1/libraw-0.21.4"}
 if !capability.Allows(policy){t.Fatal("qualified setting refused")}
 policy.DecoderVersion="rawpy-0.27.0/libraw-0.21.4"
 if capability.Allows(policy){t.Fatal("foreign decoder admitted")}
 view,ok:=capability.View();if !ok{t.Fatal("missing view")};view.Demosaic[0]="changed"
 fresh,_:=capability.View();if fresh.Demosaic[0]!="ahd"{t.Fatal("mutable capability")}
 for i:=range report.Checks {
  report.Checks[i].Passed=false
  if _,err:=verify();err==nil{t.Fatal("failed RAW check accepted",report.Checks[i].ID)}
  report.Checks[i].Passed=true
 }
 expected.RuntimeLockSHA=digest("0")
 if _,err:=verify();err==nil{t.Fatal("wrong native runtime accepted")}
 var absent *Capability
 if absent.Allows(policy){t.Fatal("absent capability admitted policy")}
}

func rawCapabilityFixture()(v2qualification.Report,v2qualification.Expected) {
 digest:=func(s string)string{return strings.Repeat(s,64)}
 issuer:=uuid.New()
 report:=v2qualification.Report{SchemaVersion:1,Operation:"asset.decode",Version:"1",
  ModelLockSHA:digest("a"),RuntimeLockSHA:digest("b"),DeviceProfileSHA:digest("c"),DatasetSHA:digest("d"),
  InputFixtureSHAs:[]string{digest("e")},Status:"passed",IssuerID:issuer,CreatedAt:time.Date(2026,10,12,0,0,0,0,time.UTC),
  Metrics:v2qualification.Metrics{P50MS:10,P95MS:20}}
 for _,id:=range requiredChecks {report.Checks=append(report.Checks,v2qualification.Check{ID:id,Passed:true,EvidenceSHA:digest("f")})}
 expected:=v2qualification.Expected{Operation:report.Operation,Version:"1",Backend:"cpu",ModelLockSHA:report.ModelLockSHA,
  RuntimeLockSHA:report.RuntimeLockSHA,DeviceProfileSHA:report.DeviceProfileSHA,DatasetSHA:report.DatasetSHA,
  IssuerID:issuer,TestFixtureSHAs:map[string]struct{}{digest("e"):{}},EvidenceSHAs:map[string]struct{}{digest("f"):{}},LicenseCleared:true}
 return report,expected
}
