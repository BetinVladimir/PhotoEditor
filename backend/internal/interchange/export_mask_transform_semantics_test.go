package interchange

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

func TestExportTransformedMaskRequiresSelectedChainOrigin(t *testing.T){
 for _,mode:=range []string{"resize","foreign_origin","broken_continuity","wrong_target","foreign_source"}{t.Run(mode,func(t *testing.T){
  _,_,working:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(working);if err!=nil{t.Fatal(err)}
  origin:=image.Canvas;target:=origin;target.Width=1;target.Height=1;target.SpaceID="resized"
  digest,err:=canonical.Hash(origin);if err!=nil{t.Fatal(err)}
  from:=origin;if mode=="foreign_origin"{from.SpaceID="foreign"};if mode=="foreign_source"{from.SourceSHA256=rawSHA([]byte("other original"))}
  steps:=[]any{map[string]any{"kind":"resize","from_canvas":from,"to_canvas":target,"invertibility":"partial","valid_region":map[string]any{"x":0,"y":0,"width":1,"height":1},"pixel_centers":"half_pixel"}}
  if mode=="broken_continuity"{steps=append(steps,map[string]any{"kind":"resize","from_canvas":origin,"to_canvas":target,"invertibility":"partial","valid_region":map[string]any{"x":0,"y":0,"width":1,"height":1},"pixel_centers":"half_pixel"})}
  hash,err:=canonical.Hash(map[string]any{"steps":steps});if err!=nil{t.Fatal(err)}
  chain,err:=canonical.Marshal(map[string]any{"steps":steps,"hash":hash});if err!=nil{t.Fatal(err)}
  ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("mask"))}
  mask:=review.MaskRef{Artifact:review.ArtifactRef{ID:ref.ArtifactID,SHA256:ref.SHA256},Canvas:target,TransformChain:chain,Selector:"manual",Confidence:1,Protected:true}
  if mode=="wrong_target"{mask.Canvas=origin}
  raw,err:=canonical.Marshal(mask);if err!=nil{t.Fatal(err)}
  entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"MaskRef"}
  semantics:=&ExportCanonicalMaskSemantics{canvases:map[string]bool{digest:true},locks:map[string]bool{},fallback:&exportSemanticsStub{}}
  err=semantics.QualifyExportDependency(context.Background(),entry,raw,[]v2draft.LayerRef{{Ref:ref,Contract:"MaskPixels"}})
  if (err==nil)!=(mode=="resize"){t.Fatalf("unexpected transform qualification: %v",err)}
 })}
}
