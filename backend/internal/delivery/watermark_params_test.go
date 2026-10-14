package delivery

import (
 "encoding/json"
 "math"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/exportpreset"
 "photoeditor/backend/internal/v2input"
)
func TestDeliveryWatermarkParamsGeometry(t *testing.T){
 image:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)};mark:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("b",64)}
 preset:=exportpreset.Watermark{AssetID:uuid.New(),Opacity:.7,Anchor:"bottom_right",MarginX:20,MarginY:10}
 raw,pin,err:=CompileDeliveryWatermarkParams(image,mark,1000,800,400,200,preset)
 if err!=nil||digest(raw)!=pin{t.Fatal(err)}
 var params WatermarkParams;if json.Unmarshal(raw,&params)!=nil||params.MarginX!=.02||params.MarginY!=.0125||params.Algorithm!="linear_alpha_v1"{t.Fatal(params)}
 for _,mode:=range []string{"overflow","unknown_anchor","nan","foreign_ref","oversized_canvas"}{
  p:=preset;i:=image;w,h:=1000,800
  switch mode{case "overflow":p.MarginY=799;case "unknown_anchor":p.Anchor="stretch";case "nan":p.Opacity=math.NaN();case "foreign_ref":i.ArtifactID=uuid.Nil;case "oversized_canvas":w=100000;h=100000}
  if _,_,err=CompileDeliveryWatermarkParams(i,mark,w,h,400,200,p);err==nil{t.Fatal(mode)}
 }
 // Tall marks must be refused rather than silently cropped or distorted.
 if _,_,err=CompileDeliveryWatermarkParams(image,mark,1000,100,10,1000,preset);err==nil{t.Fatal("tall mark overflow")}
}
