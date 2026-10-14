package delivery

import (
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestWatermarkStageWorkerInputOrderAndAlias(t *testing.T){
 scope,config,e:=watermarkPinFixture(t);pin,err:=CompileWatermarkSourcePin(scope,config,e);if err!=nil{t.Fatal(err)}
 image,err:=review.ParseWorkingImage(e.Manifest);if err!=nil{t.Fatal(err)}
 image.Canvas.Width=400;image.Canvas.Height=200
 image.Pixels=review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("e",64)}
 alpha:=review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("f",64)};image.Alpha=&alpha
 for _,hasAlpha:=range []bool{true,false}{
  if !hasAlpha{image.Alpha=nil}
  raw,err:=canonical.Marshal(image);if err!=nil{t.Fatal(err)}
  ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:digest(raw)}
  stage,err:=pin.CompileStage(scope,config,ref,raw);if err!=nil{t.Fatal(err)}
  markOffset:=2;if hasAlpha{markOffset=3}
  if len(stage.Inputs)!=markOffset+3||stage.Inputs[0]!=ref||stage.Inputs[1].ArtifactID!=image.Pixels.ID||stage.Inputs[markOffset]!=e.Source.Ref||stage.Inputs[markOffset+1]!=e.Arrays[0].Ref||stage.Inputs[markOffset+2]!=e.Arrays[1].Ref||stage.ParamsSHA!=digest(stage.Params){t.Fatal("worker input order")}
  if stage.ImagePixels!=80000||stage.WatermarkPixels!=5000||stage.ImageHasAlpha!=hasAlpha{t.Fatal("resource geometry")}
 }
 if _,err=pin.CompileStage(scope,config,e.Source.Ref,e.Manifest);err==nil{t.Fatal("input alias accepted")}
}
