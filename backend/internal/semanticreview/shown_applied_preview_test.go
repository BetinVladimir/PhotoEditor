package semanticreview
import (
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestAppliedPreviewParamsKeepEveryTargetProducer(t *testing.T){
 for _,operation:=range []string{"semantic.preview_local_exposure","semantic.preview_remove_object","semantic.preview_background_cleanup","semantic.preview_retouch_region"}{
  p:=shownAdoptionFixture();p.PreviewTaskID=uuid.NewSHA1(p.PreviewRunID,[]byte(operation+":"+p.TargetID.String()));raw,_:=canonical.Marshal(p)
  got,err:=decodeAppliedParams(raw,p.TenantID,p.TargetID);if err!=nil||got!=p{t.Fatalf("%s: %v",operation,err)}
  if _,err=decodeAppliedParams(raw,p.TenantID,uuid.New());err==nil{t.Fatal("foreign target accepted")}
  p.PreviewFence=0;raw,_=canonical.Marshal(p);if _,err=decodeAppliedParams(raw,p.TenantID,p.TargetID);err==nil{t.Fatal("missing producer fence accepted")}
 }
}
func TestAppliedPresentationRequiresExactShownPixelsAndProfile(t *testing.T){
 p:=shownAdoptionFixture();ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:p.ModelLockSHA}}
 identity,_:=json.Marshal(map[string]any{"tenant_id":p.TenantID,"target_id":p.TargetID})
 a:=appliedPresentation{SchemaVersion:1,Params:identity,NumericalEvidence:p.NumericalEvidence,WorkingImage:p.WorkingImage,Pixels:p.Pixels,Before:ref(),After:ref(),Affected:ref(),Protected:ref(),Width:200,Height:100,ICCLockSHA:p.ModelLockSHA,SRGBSHA:p.RuntimeLockSHA,Resize:"area_linear_premult_v1",Overlay:"max_coverage_ceil_alpha_v1"}
 raw,_:=json.Marshal(a);if _,err:=decodeAppliedPresentation(raw,p,p.RuntimeLockSHA);err!=nil{t.Fatal(err)}
 for _,mutate:=range []func(*appliedPresentation){func(a *appliedPresentation){a.Pixels.ArtifactID=uuid.New()},func(a *appliedPresentation){a.SRGBSHA=p.ModelLockSHA},func(a *appliedPresentation){a.Width=2561},func(a *appliedPresentation){a.Params=json.RawMessage(`{}`)}}{bad:=a;mutate(&bad);raw,_=json.Marshal(bad);if _,err:=decodeAppliedPresentation(raw,p,p.RuntimeLockSHA);err==nil{t.Fatal("foreign presentation accepted")}}
}
