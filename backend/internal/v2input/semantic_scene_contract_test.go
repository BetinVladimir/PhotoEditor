package v2input

import (
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "testing"
 "strings"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

func sceneContractFixture(t *testing.T) SemanticSceneEvidence {
 t.Helper()
 ref:=func() Ref{return Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}}
 source:=func() SemanticSceneSource{return SemanticSceneSource{Binding:SessionVersionBinding{AssetVersionID:uuid.New(),Source:ref(),CanvasHash:strings.Repeat("b",64)},Labels:ref(),LabelsParams:ref(),LabelsPixels:ref(),Depth:ref(),DepthParams:ref(),DepthPixels:ref()}}
 policy:=SemanticScenePolicy{Algorithm:"farneback_depth_plane_v1",BackgroundLabels:[]int{0,1},ProxyEdge:512,MinSupport:64,FlowErrorPX:.5,HomographyErrorPX:.5,DepthPlaneError:.01}
 raw,err:=json.Marshal(policy);if err!=nil {t.Fatal(err)};raw,err=canonical.Bytes(raw);if err!=nil {t.Fatal(err)};hash:=sha256.Sum256(raw)
 return SemanticSceneEvidence{SchemaVersion:1,Reference:source(),Target:source(),Intent:ref(),Calibration:ref(),Policy:policy,PolicySHA:hex.EncodeToString(hash[:]),ModelLockSHA:strings.Repeat("c",64),RuntimeLockSHA:strings.Repeat("d",64),DeviceProfileSHA:strings.Repeat("e",64),DatasetSHA:strings.Repeat("f",64)}
}
func TestSemanticSceneContractExactPolicyAndSources(t *testing.T) {
 original:=sceneContractFixture(t)
 raw,err:=json.Marshal(original);if err!=nil {t.Fatal(err)}
 decoded,err:=DecodeSemanticSceneEvidence(raw);if err!=nil || decoded.PolicySHA!=original.PolicySHA {t.Fatalf("valid exact evidence: %v",err)}
 for name,change:=range map[string]func(*SemanticSceneEvidence){
  "policy_changed":func(v *SemanticSceneEvidence){v.Policy.FlowErrorPX=1},
  "foreign_alias":func(v *SemanticSceneEvidence){v.Target.Depth=v.Reference.Depth},
  "same_version":func(v *SemanticSceneEvidence){v.Target.Binding.AssetVersionID=v.Reference.Binding.AssetVersionID},
  "unknown_algorithm":func(v *SemanticSceneEvidence){v.Policy.Algorithm="trust_client"},
 } {t.Run(name,func(t *testing.T){value:=sceneContractFixture(t);change(&value);body,_:=json.Marshal(value);if _,err:=DecodeSemanticSceneEvidence(body);err==nil {t.Fatal("altered scene evidence accepted")}})}
}

func TestSemanticSceneEntriesIncludePhysicalChildrenAndParams(t *testing.T) {
 e:=sceneContractFixture(t)
 raw,err:=json.Marshal(e);if err!=nil {t.Fatal(err)}
 params:=SemanticTransferParams{SchemaVersion:1,Intent:e.Intent,Reference:e.Reference.Binding,Target:e.Target.Binding,ModelLockSHA:e.ModelLockSHA,Calibration:e.Calibration}
 entries,err:=SemanticSceneEntries(raw,params);if err!=nil || len(entries)!=12 {t.Fatalf("complete typed inventory %d: %v",len(entries),err)}
 contracts:=map[string]int{};for _,entry:=range entries {contracts[entry.Contract]++}
 if contracts["SceneLabels"]!=2 || contracts["RelativeDepth"]!=2 || contracts["SceneParseParams"]!=2 || contracts["DepthParams"]!=2 || contracts["NpyArray"]!=4 {t.Fatalf("wrong inventory: %v",contracts)}
 params.Target.Source.ArtifactID=uuid.New()
 if _,err:=SemanticSceneEntries(raw,params);err==nil {t.Fatal("foreign exact source accepted")}
}

func TestSemanticScenePolicyCodecMatchesFrozenEvidence(t *testing.T) {
 evidence:=sceneContractFixture(t)
 raw,err:=canonical.Marshal(evidence.Policy);if err!=nil {t.Fatal(err)}
 _,sha,err:=DecodeSemanticScenePolicy(raw);if err!=nil || sha!=evidence.PolicySHA {t.Fatalf("policy codec mismatch: %v",err)}
 for _,body:=range [][]byte{[]byte(`null`),[]byte(`{}`),append([]byte(`{"extra":true,`),raw[1:]...),[]byte(`{"algorithm":null,"background_labels":[0],"proxy_edge":512,"min_support":64,"flow_error_px":0.5,"homography_error_px":0.5,"depth_plane_error":0.01}`)} {
  if _,_,err:=DecodeSemanticScenePolicy(body);err==nil {t.Fatal("malformed policy accepted")}
 }
}

func TestSemanticScenePolicyRefusesNullLabelInsteadOfCoercingBackgroundZero(t *testing.T) {
 raw:=[]byte(`{"algorithm":"farneback_depth_plane_v1","background_labels":[null],"proxy_edge":512,"min_support":64,"flow_error_px":0.5,"homography_error_px":0.5,"depth_plane_error":0.01}`)
 if _,_,err:=DecodeSemanticScenePolicy(raw);err==nil {t.Fatal("null label became background class zero")}
}
