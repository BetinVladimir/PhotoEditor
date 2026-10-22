package v2render

import (
 "encoding/json"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func authoredUpscaleContent(t *testing.T)(AuthoredPhaseReceipt,[]byte,[]byte,[]byte,[]byte){
 t.Helper();raw,_,_:=originalDenseFixture(t,false);source,err:=review.ParseWorkingImage(raw);if err!=nil{t.Fatal(err)}
 source.Color=review.ColorSpace{ProfileID:"work_rec2020_gamma24_v1",Encoding:"gamma24",ICCSHA256:strings.Repeat("a",64)}
 raw,_=canonical.Marshal(source);sourceSHA,_:=canonical.Hash(source);canvasSHA,_:=canonical.Hash(source.Canvas)
 binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:sourceSHA},CanvasHash:canvasSHA}
 params:=authoredUpscaleParams{Image:binding.Source,Scale:2,Model:"realesrgan_x2",Lock:authoredLinesTestSHA,Adapter:"work_srgb_delta_v1",Tile:256,Overlap:32};paramsRaw,_:=canonical.Marshal(params)
 image:=source;image.Pixels=review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("b",64)}
 if source.Alpha!=nil{image.Alpha=&review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("c",64)}}
 image.Canvas=review.Canvas{Width:source.Canvas.Width*2,Height:source.Canvas.Height*2,Orientation:1,SpaceID:"up2-"+source.Canvas.SourceSHA256[:16],SourceSHA256:source.Canvas.SourceSHA256}
 var chain struct{Steps []json.RawMessage `json:"steps"`;Hash string `json:"hash"`};_ = json.Unmarshal(source.TransformChain,&chain)
 step,_:=canonical.Marshal(map[string]any{"kind":"resize","from_canvas":source.Canvas,"to_canvas":image.Canvas,"invertibility":"invertible","valid_region":map[string]int64{"x":0,"y":0,"width":image.Canvas.Width,"height":image.Canvas.Height},"pixel_centers":"half_pixel"})
 chain.Steps=append(chain.Steps,step);chain.Hash,_=canonical.Hash(map[string]any{"steps":chain.Steps});image.TransformChain,_=canonical.Marshal(chain)
 imageRaw,_:=canonical.Marshal(image);imageSHA,_:=canonical.Hash(image);imageRef:=v2input.Ref{ArtifactID:uuid.New(),SHA256:imageSHA}
 report,_:=canonical.Marshal(map[string]any{"image":params.Image,"model_id":params.Model,"model_lock_id":params.Lock,"backend":"mps","scale":2,"color_adapter_id":params.Adapter,"tile":256,"overlap":32,"input_pixel_count":source.Canvas.Width*source.Canvas.Height,"output_pixel_count":image.Canvas.Width*image.Canvas.Height,"disabled_delta_fraction":0,"source_clip_fraction":0,"gamut_excess_fraction":0,"alpha_resampled":image.Alpha!=nil,"warnings":[]string{}})
 phase:=AuthoredPhaseReceipt{InputSource:binding,Geometry:&AuthoredGeometryPhaseWitness{Task:AuthoredPhaseTask{Operation:"image.upscale",ProfileJCS:`{"backend":"mps"}`},Root:imageRef,Image:imageRef}}
 return phase,paramsRaw,raw,imageRaw,report
}
func TestAuthoredUpscaleActualResizeGeometry(t *testing.T){
 phase,params,source,image,report:=authoredUpscaleContent(t)
 g,err:=validateAuthoredUpscaleGeometry(phase,params,source,image,report);if err!=nil||g.CanvasHash==phase.InputSource.CanvasHash{t.Fatal("actual upscale resize refused",err)}
}
func TestAuthoredUpscaleGeometrySubstitutionsRefused(t *testing.T){
 for _,name:=range []string{"canvas","source","chain","root","backend","scale","missing-field","model"}{t.Run(name,func(t *testing.T){
 phase,params,source,image,report:=authoredUpscaleContent(t)
 switch name{
 case "canvas","source","chain":
  var value review.WorkingImage;_ = json.Unmarshal(image,&value)
  if name=="canvas"{value.Canvas.Width++};if name=="source"{value.Canvas.SourceSHA256=strings.Repeat("e",64)};if name=="chain"{value.TransformChain=json.RawMessage(`{"steps":[],"hash":"`+authoredLinesTestSHA+`"}`)}
  image,_=canonical.Marshal(value)
 case "root":phase.Geometry.Root.ArtifactID=uuid.New()
 case "backend":phase.Geometry.Task.ProfileJCS=`{"backend":"cuda"}`
 case "scale","missing-field":var value map[string]any;_ = json.Unmarshal(report,&value);if name=="scale"{value["scale"]=4}else{delete(value,"disabled_delta_fraction")};report,_=canonical.Marshal(value)
 case "model":var value map[string]any;_ = json.Unmarshal(params,&value);value["model_id"]="realesrgan_x4";params,_=canonical.Marshal(value)
 }
 if _,err:=validateAuthoredUpscaleGeometry(phase,params,source,image,report);err==nil{t.Fatal("substitution accepted")}
 })}
}
