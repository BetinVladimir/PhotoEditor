package delivery

import (
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/exportpreset"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestDeliveryWatermarkImageBinding(t *testing.T){
 steps:=[]any{};chainHash,_:=canonical.Hash(map[string]any{"steps":steps});chain,_:=canonical.Marshal(map[string]any{"steps":steps,"hash":chainHash})
 ref:=func()review.ArtifactRef{return review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("a",64)}}
 image:=review.WorkingImage{Pixels:ref(),Canvas:review.Canvas{Width:1000,Height:800,Orientation:1,SpaceID:"working:test",SourceSHA256:strings.Repeat("b",64)},Color:review.ColorSpace{ProfileID:"work_rec2020_gamma24_v1",ICCSHA256:strings.Repeat("c",64),Encoding:"gamma24"},DType:"float32",TransformChain:chain}
 mark:=image;mark.Canvas.Width=100;mark.Canvas.Height=50;alpha:=ref();mark.Alpha=&alpha
 asset:=uuid.New();preset:=exportpreset.Watermark{AssetID:asset,Opacity:.5,Anchor:"bottom_right",MarginX:10,MarginY:10}
 imageRaw,_:=canonical.Marshal(image);imageRef:=v2input.Ref{ArtifactID:uuid.New(),SHA256:digest(imageRaw)}
 for _,mode:=range []string{"exact","no_alpha","icc","dtype","asset","hash","noncanonical"}{
  current:=mark;resolved:=asset
  switch mode{case "no_alpha":current.Alpha=nil;case "icc":current.Color.ICCSHA256=strings.Repeat("d",64);case "dtype":current.DType="uint16";case "asset":resolved=uuid.New()}
  raw,_:=canonical.Marshal(current);markRef:=v2input.Ref{ArtifactID:uuid.New(),SHA256:digest(raw)}
  if mode=="hash"{markRef.SHA256=strings.Repeat("e",64)}
  if mode=="noncanonical"{raw=append(raw,'\n');markRef.SHA256=digest(raw)}
  _,_,err:=CompileDeliveryWatermarkForImages(imageRef,imageRaw,markRef,raw,resolved,preset)
  if (err==nil)!=(mode=="exact"){t.Fatal(mode,err)}
 }
}
