package v2render

import (
 "encoding/json"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestAuthoredGeometryMaterializationPreservesAllFiveSettings(t *testing.T){
 ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}}
 original:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:ref(),CanvasHash:strings.Repeat("b",64)}
 current:=original;current.Source=ref();current.CanvasHash=strings.Repeat("c",64)
 other:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:ref(),CanvasHash:strings.Repeat("d",64)}
 cases:=map[string]map[string]any{
 "optical.lens":{"schema_version":1,"base":original,"strength":0.4,"profile":ref(),"focal_mm":50,"aperture":2.8},
 "optical.perspective":{"schema_version":1,"base":original,"strength":0.4,"mode":"manual","corners":[]map[string]float64{{"x":0,"y":0},{"x":1,"y":0},{"x":1,"y":1},{"x":0,"y":1}},"lines":nil,"max_crop_fraction":0.3},
 "optical.panorama":{"schema_version":1,"frames":[]v2input.SessionVersionBinding{other,original},"reference_ordinal":1,"max_pixels":1000000,"calibration":ref(),"projection":"cylindrical"},
 "optical.focus_stack":{"schema_version":1,"frames":[]v2input.SessionVersionBinding{other,original},"reference_ordinal":1,"max_pixels":1000000,"calibration":ref(),"motion_policy":"reject"},
 "image.upscale":{"image":original.Source,"scale":2,"model_id":"realesrgan_x2","model_lock_id":strings.Repeat("e",64),"color_adapter_id":"work_srgb_delta_v1","tile":256,"overlap":32},
 }
 for operation,fields:=range cases{t.Run(operation,func(t *testing.T){
  raw,err:=canonical.Marshal(fields);if err!=nil{t.Fatal(err)};sha,err:=canonical.Hash(fields);if err!=nil{t.Fatal(err)}
  sourceRef:=v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}
  got,err:=MaterializeAuthoredGeometry(operation,sourceRef,raw,original,current);if err!=nil{t.Fatal(err)}
  var after map[string]json.RawMessage;if json.Unmarshal(got.Body,&after)!=nil{t.Fatal("body")}
  var before map[string]json.RawMessage;if json.Unmarshal(raw,&before)!=nil{t.Fatal("original")}
  field:="base";if operation=="image.upscale"{field="image"}else if operation=="optical.panorama"||operation=="optical.focus_stack"{
   field="frames";var frames []v2input.SessionVersionBinding;if json.Unmarshal(after[field],&frames)!=nil||len(frames)!=2||frames[0]!=other||frames[1]!=current{t.Fatal("nonreference substitution")}
  }
  delete(before,field);delete(after,field);a,_:=canonical.Marshal(before);b,_:=canonical.Marshal(after);if string(a)!=string(b){t.Fatal("settings changed")}
  if got.Authored!=sourceRef||got.Source!=current||got.SHA256==sha{t.Fatal("binding")}
  wrong:=current;wrong.AssetVersionID=uuid.New();if _,e:=MaterializeAuthoredGeometry(operation,sourceRef,raw,original,wrong);e==nil{t.Fatal("foreign source")}
  broken:=sourceRef;broken.SHA256=strings.Repeat("f",64);if _,e:=MaterializeAuthoredGeometry(operation,broken,raw,original,current);e==nil{t.Fatal("unverified params")}
 })}
}
