package delivery

import (
 "bytes"
 "encoding/json"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func watermarkPinFixture(t *testing.T)(WatermarkPinScope,[]byte,WatermarkSourceEvidence){
 t.Helper();asset:=uuid.New();version:=uuid.New()
 steps:=[]any{};hash,_:=canonical.Hash(map[string]any{"steps":steps});chain,_:=canonical.Marshal(map[string]any{"steps":steps,"hash":hash})
 pixel:=review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("a",64)};alpha:=review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("b",64)}
 image:=review.WorkingImage{Pixels:pixel,Alpha:&alpha,Canvas:review.Canvas{Width:100,Height:50,Orientation:1,SpaceID:"watermark:test",SourceSHA256:strings.Repeat("c",64)},Color:review.ColorSpace{ProfileID:"work_rec2020_gamma24_v1",ICCSHA256:strings.Repeat("d",64),Encoding:"gamma24"},DType:"float32",TransformChain:chain}
 raw,err:=canonical.Marshal(image);if err!=nil{t.Fatal(err)};ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:digest(raw)};canvas,_:=canonical.Hash(image.Canvas)
 config,err:=canonical.Marshal(map[string]any{"format":"jpeg","icc_id":"srgb_v1","quality":92,"subsampling":"444","alpha":"matte","matte_rgb":[]int{1,1,1},"metadata_policy":"strip","naming_template":"{original_name}_edited","watermark":map[string]any{"asset_id":asset,"opacity":.5,"anchor":"center","margin_x":0,"margin_y":0}});if err!=nil{t.Fatal(err)}
 scope:=WatermarkPinScope{uuid.New(),uuid.New(),uuid.New(),uuid.New(),uuid.New(),digest(config)}
 evidence:=WatermarkSourceEvidence{Source:WatermarkSource{Asset:asset,Version:version,Ref:ref},Binding:v2input.SessionVersionBinding{AssetVersionID:version,Source:ref,CanvasHash:canvas},Manifest:raw,Arrays:[]WatermarkArrayObject{
  {Contract:"WorkingPixels",Ref:v2input.Ref{ArtifactID:pixel.ID,SHA256:pixel.SHA256},Object:objectstore.Object{SHA256:pixel.SHA256,Bytes:100*50*3*4+128}},
  {Contract:"WorkingAlpha",Ref:v2input.Ref{ArtifactID:alpha.ID,SHA256:alpha.SHA256},Object:objectstore.Object{SHA256:alpha.SHA256,Bytes:100*50*2+128}},
 }}
 return scope,config,evidence
}
func TestWatermarkPinRestoreOwnershipAndParams(t *testing.T){
 scope,config,evidence:=watermarkPinFixture(t)
 pin,err:=CompileWatermarkSourcePin(scope,config,evidence);if err!=nil{t.Fatal(err)}
 restored,err:=RestoreWatermarkSourcePin(pin.JCS(),pin.SHA256(),scope,config,evidence);if err!=nil{t.Fatal(err)}
 if _,_,err=restored.CompileParams(scope,config,evidence.Source.Ref,evidence.Manifest);err!=nil{t.Fatal(err)}
 original:=pin.JCS();copy:=pin.JCS();copy[0]='!';evidence.Manifest[0]='!';evidence.Arrays[0].Object.Bytes++
 if !bytes.Equal(pin.JCS(),original){t.Fatal("pin aliases caller buffers")}
 foreign:=scope;foreign.Parent=uuid.New()
 if _,_,err=restored.CompileParams(foreign,config,v2input.Ref{},nil);err==nil{t.Fatal("foreign scope")}
 if _,_,err=(FrozenWatermarkSource{}).CompileParams(scope,config,v2input.Ref{},nil);err==nil{t.Fatal("zero pin")}
}
func TestWatermarkPinRejectsSubstitution(t *testing.T){
 for _,mode:=range []string{"config_sha","asset","version","canvas","array_ref","array_size","order","manifest"}{t.Run(mode,func(t *testing.T){
  scope,config,e:=watermarkPinFixture(t)
  switch mode{case "config_sha":scope.PresetSHA=strings.Repeat("e",64);case "asset":e.Source.Asset=uuid.New();case "version":e.Binding.AssetVersionID=uuid.New();case "canvas":e.Binding.CanvasHash=strings.Repeat("f",64);case "array_ref":e.Arrays[0].Ref.ArtifactID=uuid.New();case "array_size":e.Arrays[1].Object.Bytes=1;case "order":e.Arrays[0],e.Arrays[1]=e.Arrays[1],e.Arrays[0];case "manifest":e.Manifest=append(e.Manifest,'\n')}
  if _,err:=CompileWatermarkSourcePin(scope,config,e);err==nil{t.Fatal(mode)}
 })}
}

func TestWatermarkPinRejectsCanvasResourceOverflow(t *testing.T){
 for _,geometry:=range [][2]int64{{100001,1},{1,100001},{20000,20000},{100000,100000}}{t.Run("canvas_resource_limit",func(t *testing.T){
  scope,config,e:=watermarkPinFixture(t)
  image,err:=review.ParseWorkingImage(e.Manifest);if err!=nil{t.Fatal(err)}
  image.Canvas.Width=geometry[0];image.Canvas.Height=geometry[1]
  e.Manifest,err=canonical.Marshal(image);if err!=nil{t.Fatal(err)}
  e.Source.Ref.SHA256=digest(e.Manifest);e.Binding.Source=e.Source.Ref
  e.Binding.CanvasHash,err=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
  // Valid-looking byte counts cannot authorize an oversized source canvas.
  e.Arrays[0].Object.Bytes=geometry[0]*geometry[1]*12+128
  e.Arrays[1].Object.Bytes=geometry[0]*geometry[1]*2+128
  if _,err=CompileWatermarkSourcePin(scope,config,e);err==nil{t.Fatal("oversized watermark canvas accepted")}
 })}
}

func TestWatermarkPersistedPinRecovery(t *testing.T){
 scope,config,e:=watermarkPinFixture(t)
 original,err:=CompileWatermarkSourcePin(scope,config,e);if err!=nil{t.Fatal(err)}
 raw:=original.JCS()
 restored,err:=DecodeWatermarkSourcePin(raw,original.SHA256(),scope,config);if err!=nil{t.Fatal(err)}
 raw[0]='!'
 if !bytes.Equal(restored.JCS(),original.JCS()){t.Fatal("recovered pin aliases caller")}
 foreign:=scope;foreign.Preset=uuid.New()
 if _,err=DecodeWatermarkSourcePin(original.JCS(),original.SHA256(),foreign,config);err==nil{t.Fatal("foreign preset recovered")}
 for _,mode:=range []string{"unknown","noncanonical","trailing","array_size","version","schema"}{t.Run(mode,func(t *testing.T){
  var body map[string]any;if err:=json.Unmarshal(original.JCS(),&body);err!=nil{t.Fatal(err)}
  switch mode{
  case "unknown":body["ignored"]=true
  case "array_size":body["arrays"].([]any)[0].(map[string]any)["bytes"]=1
  case "version":body["binding"].(map[string]any)["asset_version_id"]=uuid.Nil.String()
  case "schema":body["schema_version"]=2
  }
  changed,err:=canonical.Marshal(body);if err!=nil{t.Fatal(err)}
  if mode=="noncanonical"{changed=append(changed,'\n')}
  if mode=="trailing"{changed=append(changed,[]byte("{}")...)}
  if _,err=DecodeWatermarkSourcePin(changed,digest(changed),scope,config);err==nil{t.Fatal("invalid persisted pin accepted",mode)}
 })}
}
