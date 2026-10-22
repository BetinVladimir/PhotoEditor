package v2region

import (
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

func executionFixture(t *testing.T) RegionExecutionDescriptor {
 t.Helper()
 ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:regionSHA}}
 source:=ref();canvas:=review.Canvas{Width:10,Height:8,Orientation:1,SpaceID:"work",SourceSHA256:regionSHA}
 target:=canvas;target.Width=20;target.Height=16;target.SpaceID="upscale"
 transform,err:=canonical.Marshal(map[string]any{"kind":"resize","from_canvas":canvas,"to_canvas":target,"invertibility":"invertible","valid_region":map[string]int{"x":0,"y":0,"width":20,"height":16},"pixel_centers":"half_pixel"})
 if err!=nil{t.Fatal(err)}
 first:=RegionExecutionStage{Operation:"image.color",OperationVersion:1,TaskID:uuid.New(),ResultID:uuid.New(),Attempt:1,FencingToken:1,Params:ref(),Source:source,Output:ref(),SourceCanvas:canvas,TargetCanvas:canvas}
 second:=RegionExecutionStage{Operation:"image.upscale",OperationVersion:1,TaskID:uuid.New(),ResultID:uuid.New(),Attempt:1,FencingToken:2,Params:ref(),Source:first.Output,Output:ref(),SourceCanvas:canvas,TargetCanvas:target,Transform:json.RawMessage(transform)}
 return RegionExecutionDescriptor{SchemaVersion:1,Source:source,SourceCanvasHash:regionSHA,Style:ref(),Stages:[]RegionExecutionStage{first,second}}
}

func TestRegionExecutionPreservesIdentityEdgesBeforeRealUpscale(t *testing.T){
 descriptor:=executionFixture(t);raw,err:=canonical.Marshal(descriptor);if err!=nil{t.Fatal(err)}
 decoded,err:=decodeRegionExecution(raw);if err!=nil{t.Fatal(err)}
 if len(decoded.Stages)!=2||decoded.Stages[0].Transform!=nil||decoded.Stages[1].TargetCanvas.Width!=20{t.Fatal("geometry lost")}
}

func TestRegionExecutionRefusesSourceReplacementMissingTransformAndReplayReuse(t *testing.T){
 for _,mutate:=range []func(*RegionExecutionDescriptor){
  func(d *RegionExecutionDescriptor){d.Stages[1].Source=d.Source},
  func(d *RegionExecutionDescriptor){d.Stages[1].Transform=nil},
  func(d *RegionExecutionDescriptor){d.Stages[1].TaskID=d.Stages[0].TaskID},
  func(d *RegionExecutionDescriptor){d.Stages[1].SourceCanvas.Width++},
 }{
  descriptor:=executionFixture(t);mutate(&descriptor);raw,err:=canonical.Marshal(descriptor);if err!=nil{t.Fatal(err)}
  if _,err:=decodeRegionExecution(raw);err==nil{t.Fatal("invalid descriptor accepted")}
 }
}
