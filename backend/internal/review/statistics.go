package review
import (
 "errors"
 "math"
 "encoding/json"
 "bytes"
 "photoeditor/backend/internal/canonical"
)

// Stats20 is processing statistics, not proof of its source or producer.
// A legacy null source is structurally valid but cannot be transported as a
// source-bound interchange dependency without independent materialization.
type Stats20 struct {
 SourceImage *ArtifactRef `json:"source_image"`
 FeatureVersion string `json:"feature_version"`
 Values []float64 `json:"values"`
 CoverageBins []bool `json:"coverage_bins"`
 MaskConfidence float64 `json:"mask_confidence"`
 Warnings []string `json:"warnings"`
}
func ParseStats20(raw []byte)(Stats20,error) {
 invalid:=errors.New("invalid Stats20")
 var stats Stats20
 if decodeExact(raw,&stats)!=nil||stats.FeatureVersion!="stats20_v1"||len(stats.Values)!=20||len(stats.CoverageBins)!=512||
  stats.Warnings==nil||len(stats.Warnings)>64||math.IsNaN(stats.MaskConfidence)||math.IsInf(stats.MaskConfidence,0)||stats.MaskConfidence<0||stats.MaskConfidence>1 {
  return Stats20{},invalid
 }
 if stats.SourceImage!=nil&&!stats.SourceImage.valid(){return Stats20{},invalid}
 for _,value:=range stats.Values {if math.IsNaN(value)||math.IsInf(value,0){return Stats20{},invalid}}
 for _,warning:=range stats.Warnings {if !spaceIDPattern.MatchString(warning){return Stats20{},invalid}}
 return stats,nil
}

// StatsParams binds statistics to an explicit source and person mask. Parsing
// does not attest mask bytes, producer qualification or current source access.
type StatsParams struct {
 Image ArtifactRef `json:"image"`
 Person MaskRef `json:"person"`
}
func ParseStatsParams(raw []byte)(StatsParams,error){
 var params StatsParams
 if decodeExact(raw,&params)!=nil||!params.Image.valid(){return StatsParams{},errors.New("invalid stats Params")}
 maskRaw,err:=json.Marshal(params.Person);if err!=nil{return StatsParams{},err}
 mask,err:=ParseMaskRef(maskRaw)
 if err!=nil||mask.Selector!="person"{return StatsParams{},errors.New("invalid stats person mask")}
 params.Person=mask
 return params,nil
}
func(p StatsParams)ValidateSource(image WorkingImage,ref ArtifactRef)error{
 if p.Image!=ref||!ref.valid()||p.Person.Canvas!=image.Canvas{return errors.New("stale stats source")}
 a,err:=canonical.Bytes(p.Person.TransformChain);if err!=nil{return err}
 b,err:=canonical.Bytes(image.TransformChain);if err!=nil||!bytes.Equal(a,b){return errors.New("stale stats transform")}
 return nil
}
