package delivery

import (
 "math"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/exportpreset"
 "photoeditor/backend/internal/v2input"
)

type WatermarkParams struct {
 Image v2input.Ref `json:"image"`
 Watermark v2input.Ref `json:"watermark"`
 Opacity float64 `json:"opacity"`
 Anchor string `json:"anchor"`
 MarginX float64 `json:"margin_x"`
 MarginY float64 `json:"margin_y"`
 MaxWidthFraction float64 `json:"max_width_fraction"`
 Algorithm string `json:"algorithm"`
}
// CompileDeliveryWatermarkParams maps pixel preset margins into the existing
// derivative.watermark contract. Refs and dimensions must come from independent
// authorized WorkingImage verification; this value compiler proves no IO/ACL.
func CompileDeliveryWatermarkParams(image,mark v2input.Ref,width,height,markWidth,markHeight int,preset exportpreset.Watermark)([]byte,string,error){
 invalid:=func()([]byte,string,error){return nil,"",v2input.ErrInvalid}
 if image.ArtifactID==uuid.Nil||mark.ArtifactID==uuid.Nil||!shaPattern.MatchString(image.SHA256)||!shaPattern.MatchString(mark.SHA256)||
 preset.AssetID==uuid.Nil||math.IsNaN(preset.Opacity)||math.IsInf(preset.Opacity,0)||preset.Opacity<0||preset.Opacity>1||
 width<1||height<1||width>100000||height>100000||int64(width)*int64(height)>200000000||
 markWidth<1||markHeight<1||markWidth>100000||markHeight>100000||int64(markWidth)*int64(markHeight)>200000000||
 preset.MarginX<0||preset.MarginY<0||preset.MarginX>width||preset.MarginY>height{return invalid()}
 scale:=math.Min(1,float64(width)*.25/float64(markWidth))
 mw:=int(math.Max(1,math.RoundToEven(float64(markWidth)*scale)))
 mh:=int(math.Max(1,math.RoundToEven(float64(markHeight)*scale)))
 x,y:=preset.MarginX,preset.MarginY
 switch preset.Anchor{
 case "top_left":
 case "top_right":x=width-mw-preset.MarginX
 case "bottom_left":y=height-mh-preset.MarginY
 case "bottom_right":x=width-mw-preset.MarginX;y=height-mh-preset.MarginY
 case "center":x=(width-mw)/2;y=(height-mh)/2
 default:return invalid()
 }
 if x<0||y<0||x+mw>width||y+mh>height{return invalid()}
 raw,err:=canonical.Marshal(WatermarkParams{image,mark,preset.Opacity,preset.Anchor,float64(preset.MarginX)/float64(width),float64(preset.MarginY)/float64(height),.25,"linear_alpha_v1"})
 if err!=nil{return invalid()};return raw,digest(raw),nil
}
