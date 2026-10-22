package semanticproposal

import (
 "encoding/json"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

func sceneDeploymentPolicyFixture() createScenePolicy {
 sha:=strings.Repeat("a",64)
 expected:=func(operation string) createExpectation{return createExpectation{Operation:operation,Version:"1",Backend:"cuda",ModelLockSHA:sha,RuntimeLockSHA:sha,DeviceProfileSHA:sha,DatasetSHA:sha,IssuerID:uuid.New(),RequiredChecks:[]string{"physical"},CalibrationSHAs:[]string{sha},TestFixtureSHAs:[]string{sha},EvidenceSHAs:[]string{sha},LicenseCleared:true}}
 return createScenePolicy{Policy:json.RawMessage(`{"algorithm":"farneback_depth_plane_v1","background_labels":[0],"proxy_edge":32,"min_support":32,"flow_error_px":1,"homography_error_px":1,"depth_plane_error":0.01}`),Scene:createSceneProducer{Report:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},Expected:expected("scene.parse"),Costs:createResourceCosts{RAM:11,VRAM:12,Scratch:13}},Depth:createSceneProducer{Report:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},Expected:expected("depth.predict"),Costs:createResourceCosts{RAM:21,VRAM:22,Scratch:23}}}
}
func TestCreateSceneDeploymentMountsIndependentOwnedProducerPolicies(t *testing.T) {
 sha:=strings.Repeat("a",64)
 policy:=sceneDeploymentPolicyFixture()
 prepared,err:=policy.preparation();if err!=nil{t.Fatal(err)}
 if prepared.Scene.Report!=policy.Scene.Report || prepared.Depth.Report!=policy.Depth.Report || prepared.Scene.Expected.Operation!="scene.parse" || prepared.Depth.Expected.Operation!="depth.predict" || prepared.Scene.Costs.RAMBytes!=11 || prepared.Depth.Costs.ScratchBytes!=23 {t.Fatal("operator producer authority or measured costs lost")}
 policy.Policy[2]='X';policy.Scene.Expected.RequiredChecks[0]="mutated";policy.Depth.Expected.EvidenceSHAs[0]=strings.Repeat("b",64)
 if prepared.Policy[2]=='X' || prepared.Scene.Expected.RequiredChecks[0]!="physical" {t.Fatal("operator input aliases installed policy")}
 if _,ok:=prepared.Depth.Expected.EvidenceSHAs[sha];!ok{t.Fatal("depth evidence aliases operator input")}
}

func TestCreateSceneDeploymentRejectsMalformedScientificPolicy(t *testing.T) {
 for _,raw:=range []string{`null`,`{"algorithm":"farneback_depth_plane_v1"}`,`{"algorithm":"farneback_depth_plane_v1","background_labels":[null],"proxy_edge":32,"min_support":32,"flow_error_px":1,"homography_error_px":1,"depth_plane_error":0.01}`} {
  if _,err:=(createScenePolicy{Policy:json.RawMessage(raw)}).preparation();err==nil{t.Fatal("malformed operator scene policy admitted")}
 }
}
