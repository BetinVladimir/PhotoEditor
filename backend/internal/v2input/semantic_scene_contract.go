package v2input

import (
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "photoeditor/backend/internal/canonical"
)

// SemanticSceneEvidence is producer-bound evidence, not a client assertion of
// planarity. SQL must independently prove every source and canonical producer.
type SemanticSceneSource struct {
 Binding SessionVersionBinding `json:"binding"`
 Labels Ref `json:"labels"`
 LabelsParams Ref `json:"labels_params"`
 LabelsPixels Ref `json:"labels_pixels"`
 Depth Ref `json:"depth"`
 DepthParams Ref `json:"depth_params"`
 DepthPixels Ref `json:"depth_pixels"`
 DepthInvalid *Ref `json:"depth_invalid"`
}
type SemanticScenePolicy struct {
 Algorithm string `json:"algorithm"`
 BackgroundLabels []int `json:"background_labels"`
 ProxyEdge int `json:"proxy_edge"`
 MinSupport int `json:"min_support"`
 FlowErrorPX float64 `json:"flow_error_px"`
 HomographyErrorPX float64 `json:"homography_error_px"`
 DepthPlaneError float64 `json:"depth_plane_error"`
}
type SemanticSceneEvidence struct {
 SchemaVersion int `json:"schema_version"`
 Reference SemanticSceneSource `json:"reference"`
 Target SemanticSceneSource `json:"target"`
 Intent Ref `json:"intent"`
 Calibration Ref `json:"calibration"`
 Policy SemanticScenePolicy `json:"policy"`
 PolicySHA string `json:"policy_sha"`
 ModelLockSHA string `json:"model_lock_sha"`
 RuntimeLockSHA string `json:"runtime_lock_sha"`
 DeviceProfileSHA string `json:"device_profile_sha"`
 DatasetSHA string `json:"dataset_sha"`
}

func DecodeSemanticSceneEvidence(raw []byte) (SemanticSceneEvidence,error) {
 var out SemanticSceneEvidence
 fields,ok:=semanticObject(raw,nil,"schema_version","reference","target","intent","calibration","policy","policy_sha","model_lock_sha","runtime_lock_sha","device_profile_sha","dataset_sha")
 if len(raw)>1<<20 || !ok || !semanticDecode(raw,&out) {return SemanticSceneEvidence{},ErrInvalid}
 for _,key:=range []string{"reference","target"} {
  source,valid:=semanticObject(fields[key],map[string]bool{"depth_invalid":true},"binding","labels","labels_params","labels_pixels","depth","depth_params","depth_pixels","depth_invalid")
  if !valid || !semanticBindingJSON(source["binding"]) || !semanticRefJSON(source["labels"]) || !semanticRefJSON(source["depth"]) || !semanticRefJSON(source["labels_params"]) || !semanticRefJSON(source["depth_params"]) || !semanticRefJSON(source["labels_pixels"]) || !semanticRefJSON(source["depth_pixels"]) {return SemanticSceneEvidence{},ErrInvalid}
 }
 if !semanticRefJSON(fields["intent"]) || !semanticRefJSON(fields["calibration"]) || !validRef(out.Intent) || !validRef(out.Calibration) || out.SchemaVersion!=1 {return SemanticSceneEvidence{},ErrInvalid}
 _,policySHA,err:=DecodeSemanticScenePolicy(fields["policy"])
 if err!=nil || policySHA!=out.PolicySHA {return SemanticSceneEvidence{},ErrInvalid}
 for _,digest:=range []string{out.PolicySHA,out.ModelLockSHA,out.RuntimeLockSHA,out.DeviceProfileSHA,out.DatasetSHA} {if !digestPattern.MatchString(digest) {return SemanticSceneEvidence{},ErrInvalid}}
 for _,source:=range []SemanticSceneSource{out.Reference,out.Target} {
  if !semanticBindingValid(source.Binding) || !validRef(source.Labels) || !validRef(source.Depth) || !validRef(source.LabelsParams) || !validRef(source.DepthParams) || !validRef(source.LabelsPixels) || !validRef(source.DepthPixels) || (source.DepthInvalid!=nil && !validRef(*source.DepthInvalid)) {return SemanticSceneEvidence{},ErrInvalid}
  if source.Labels.ArtifactID==source.Depth.ArtifactID {return SemanticSceneEvidence{},ErrInvalid}
 }
 a,b:=out.Reference,out.Target
 if a.Binding.AssetVersionID==b.Binding.AssetVersionID && a.Binding!=b.Binding {return SemanticSceneEvidence{},ErrInvalid}
 shared:=a.Labels.ArtifactID==b.Labels.ArtifactID || a.Labels.ArtifactID==b.Depth.ArtifactID || a.Depth.ArtifactID==b.Labels.ArtifactID || a.Depth.ArtifactID==b.Depth.ArtifactID
 if shared && (a.Binding.Source!=b.Binding.Source || a.Binding.CanvasHash!=b.Binding.CanvasHash || a.Labels!=b.Labels || a.Depth!=b.Depth || a.LabelsParams!=b.LabelsParams || a.DepthParams!=b.DepthParams) {return SemanticSceneEvidence{},ErrInvalid}
 return out,nil
}

// SemanticSceneEntries freezes every physical scene/depth child explicitly.
// Canonical producer/source authority remains a separate SQL admission check.
func SemanticSceneEntries(raw []byte, params SemanticTransferParams) ([]Entry,error) {
 e,err:=DecodeSemanticSceneEvidence(raw)
 if err!=nil || e.Reference.Binding!=params.Reference || e.Target.Binding!=params.Target || e.Intent!=params.Intent || e.Calibration!=params.Calibration || e.ModelLockSHA!=params.ModelLockSHA {return nil,ErrInvalid}
 var entries []Entry
 for _,source:=range []SemanticSceneSource{e.Reference,e.Target} {
  entries=append(entries,Entry{source.Labels,"SceneLabels"},Entry{source.LabelsParams,"SceneParseParams"},Entry{source.LabelsPixels,"NpyArray"},Entry{source.Depth,"RelativeDepth"},Entry{source.DepthParams,"DepthParams"},Entry{source.DepthPixels,"NpyArray"})
  if source.DepthInvalid!=nil {entries=append(entries,Entry{*source.DepthInvalid,"NpyArray"})}
 }
 return entries,nil
}

// DecodeSemanticScenePolicy shares one strict policy codec between trusted
// preadmission configuration and frozen worker/adopter evidence.
func DecodeSemanticScenePolicy(raw []byte) (SemanticScenePolicy,string,error) {
 var p SemanticScenePolicy
 if len(raw)>16384 {return p,"",ErrInvalid}
 fields,ok:=semanticObject(raw,nil,"algorithm","background_labels","proxy_edge","min_support","flow_error_px","homography_error_px","depth_plane_error")
 if !ok || !semanticDecode(raw,&p) {return SemanticScenePolicy{},"",ErrInvalid}
 // encoding/json otherwise maps a null integer array element to zero.
 var labels []json.RawMessage
 if json.Unmarshal(fields["background_labels"],&labels)!=nil {return SemanticScenePolicy{},"",ErrInvalid}
 for _,label:=range labels {var value *int;if json.Unmarshal(label,&value)!=nil || value==nil {return SemanticScenePolicy{},"",ErrInvalid}}
 if p.Algorithm!="farneback_depth_plane_v1" || p.ProxyEdge<32 || p.ProxyEdge>1024 || p.MinSupport<32 || p.MinSupport>p.ProxyEdge*p.ProxyEdge || len(p.BackgroundLabels)<1 || len(p.BackgroundLabels)>150 || !finiteRange(p.FlowErrorPX,0,2) || p.FlowErrorPX==0 || !finiteRange(p.HomographyErrorPX,0,2) || p.HomographyErrorPX==0 || !finiteRange(p.DepthPlaneError,0,.05) || p.DepthPlaneError==0 {return SemanticScenePolicy{},"",ErrInvalid}
 for i,label:=range p.BackgroundLabels {if label<0 || label>=150 || (i>0 && label<=p.BackgroundLabels[i-1]) {return SemanticScenePolicy{},"",ErrInvalid}}
 body,err:=json.Marshal(p);if err!=nil {return SemanticScenePolicy{},"",ErrInvalid};body,err=canonical.Bytes(body);if err!=nil {return SemanticScenePolicy{},"",ErrInvalid}
 digest:=sha256.Sum256(body)
 return p,hex.EncodeToString(digest[:]),nil
}
