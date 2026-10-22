package v2render

import (
 "bytes"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "io"
 "math"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)

// MaterializedAuthoredGeometry owns real Params bytes, not a provisional output
// or an admitted task. The original occurrence remains independently retained.
type MaterializedAuthoredGeometry struct {
 Authored v2input.Ref
 Source v2input.SessionVersionBinding
 Body json.RawMessage
 SHA256 string
}
func authoredBindingValid(b v2input.SessionVersionBinding)bool{
 if b.AssetVersionID==uuid.Nil||b.Source.ArtifactID==uuid.Nil{return false}
 for _,value:=range []string{b.Source.SHA256,b.CanvasHash}{if _,err:=domain.ParseDigest(value);err!=nil{return false}}
 return true
}
func decodeAuthoredParams(raw []byte,target any)error{
 d:=json.NewDecoder(bytes.NewReader(raw));d.DisallowUnknownFields();if err:=d.Decode(target);err!=nil{return err}
 var tail any;if d.Decode(&tail)!=io.EOF{return v2input.ErrInvalid};return nil
}

// MaterializeAuthoredGeometry changes only the designated current source. It
// reuses real typed perspective/multi compilers; all nonreference frames and
// every authored algorithm/model/calibration setting are retained. SQL source
// scope, operation qualification, transport publication and sealing are separate.
func MaterializeAuthoredGeometry(operation string,ref v2input.Ref,raw []byte,original,current v2input.SessionVersionBinding)(MaterializedAuthoredGeometry,error){
 fail:=func()(MaterializedAuthoredGeometry,error){return MaterializedAuthoredGeometry{},v2input.ErrInvalid}
 if !authoredBindingValid(original)||!authoredBindingValid(current)||current.AssetVersionID!=original.AssetVersionID||ref.ArtifactID==uuid.Nil||len(raw)<2||len(raw)>1048576{return fail()}
 sum:=sha256.Sum256(raw);if hex.EncodeToString(sum[:])!=ref.SHA256{return fail()}
 wire,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(wire,raw){return fail()}
 var body []byte
 switch operation{
 case "optical.lens":
  var p v2input.OpticalLensParams
  if decodeAuthoredParams(raw,&p)!=nil||p.Base!=original||p.SchemaVersion!=1||p.Profile.ArtifactID==uuid.Nil||math.IsNaN(p.Strength)||math.IsInf(p.Strength,0)||p.Strength<0||p.Strength>1||math.IsNaN(p.FocalMM)||math.IsInf(p.FocalMM,0)||p.FocalMM<1||p.FocalMM>2000||math.IsNaN(p.Aperture)||math.IsInf(p.Aperture,0)||p.Aperture<0.5||p.Aperture>128{return fail()}
  if _,err=domain.ParseDigest(p.Profile.SHA256);err!=nil{return fail()};p.Base=current;body,err=canonical.Marshal(p)
 case "optical.perspective":
  p,e:=v2optical.ParsePerspectiveParams(raw);if e!=nil||p.Base!=original{return fail()};p.Base=current;body,_,err=p.Compile()
 case "optical.panorama":
  p,e:=v2optical.ParsePanoramaParams(raw);if e!=nil||p.Frames[p.ReferenceOrdinal]!=original{return fail()}
  p.Frames=append([]v2input.SessionVersionBinding(nil),p.Frames...);p.Frames[p.ReferenceOrdinal]=current;body,_,err=p.Compile()
 case "optical.focus_stack":
  p,e:=v2optical.ParseFocusStackParams(raw);if e!=nil||p.Frames[p.ReferenceOrdinal]!=original{return fail()}
  p.Frames=append([]v2input.SessionVersionBinding(nil),p.Frames...);p.Frames[p.ReferenceOrdinal]=current;body,_,err=p.Compile()
 case "image.upscale":
  // Exact current UpscaleParams ABI, including its schema defaults. This is
  // structural validation only: an operator-qualified model remains mandatory.
  p:=struct{Image v2input.Ref `json:"image"`;Scale int `json:"scale"`;Model string `json:"model_id"`;Lock string `json:"model_lock_id"`;Color string `json:"color_adapter_id"`;Tile int `json:"tile"`;Overlap int `json:"overlap"`}{Color:"work_srgb_delta_v1",Tile:256,Overlap:32}
  if decodeAuthoredParams(raw,&p)!=nil||p.Image!=original.Source||(p.Scale!=2&&p.Scale!=4)||p.Color!="work_srgb_delta_v1"||p.Tile!=256||p.Overlap!=32{return fail()}
  expected:="realesrgan_x2";if p.Scale==4{expected="realesrgan_x4"};if p.Model!=expected{return fail()}
  if _,err=domain.ParseDigest(p.Lock);err!=nil{return fail()};p.Image=current.Source;body,err=canonical.Marshal(p)
 default:return fail()
 }
 if err!=nil{return fail()};digest:=sha256.Sum256(body)
 return MaterializedAuthoredGeometry{Authored:ref,Source:current,Body:body,SHA256:hex.EncodeToString(digest[:])},nil
}
