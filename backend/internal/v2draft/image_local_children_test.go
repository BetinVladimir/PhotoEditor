package v2draft

import (
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

func TestImageLocalRegionInversionTypedClosure(t *testing.T){
 ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
 hash,err:=canonical.Hash(map[string]any{"steps":[]any{}});if err!=nil{t.Fatal(err)}
 chain,err:=canonical.Marshal(map[string]any{"steps":[]any{},"hash":hash});if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"omitted","false","true","null","string","protected","selector","unknown"}{t.Run(mode,func(t *testing.T){
  mask:=review.MaskRef{Artifact:review.ArtifactRef{ID:uuid.New(),SHA256:ref().SHA256},Canvas:review.Canvas{Width:2,Height:3,Orientation:1,SpaceID:"source",SourceSHA256:ref().SHA256},TransformChain:chain,Selector:"person",Confidence:1}
  if mode=="protected"{mask.Protected=true};if mode=="selector"{mask.Selector="manual"}
  region:=map[string]any{"region_id":"background","mask":mask}
  switch mode{case "false":region["invert"]=false;case "true","protected","selector":region["invert"]=true;case "null":region["invert"]=nil;case "string":region["invert"]="true";case "unknown":region["invert"]=false;region["override"]=true}
  value:=map[string]any{"image":ref(),"base_aligned":ref(),"resolved":ref(),"regions":[]any{region}}
  raw,err:=canonical.Marshal(value);if err!=nil{t.Fatal(err)};parent:=ref()
  refs,err:=DocumentChildren(raw,"Params",parent)
  valid:=mode=="omitted"||mode=="false"||mode=="true"
  if !valid{if err==nil{t.Fatal("invalid inversion accepted")};return}
  if err!=nil||len(refs)!=4{t.Fatal(refs,err)}
  child:=refs[3]
  if child.Contract!="MaskPixels"||child.Ref.ArtifactID!=mask.Artifact.ID||child.Path!="Document.regions[0].mask.artifact"||child.ParentRef!=parent{t.Fatal("mask provenance lost",child)}
 })}
}
