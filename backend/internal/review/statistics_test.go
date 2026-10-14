package review
import (
 "testing"
 "photoeditor/backend/internal/canonical"
 "github.com/google/uuid"
)
func TestStats20ClosedProcessingContract(t *testing.T){
 for _,mode:=range []string{"valid","legacy","values","coverage","null-warning","warning","confidence","unknown","source"}{t.Run(mode,func(t *testing.T){
  value:=map[string]any{"source_image":ArtifactRef{ID:uuid.New(),SHA256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"feature_version":"stats20_v1","values":make([]float64,20),"coverage_bins":make([]bool,512),"mask_confidence":1,"warnings":[]string{}}
  switch mode{case "legacy":value["source_image"]=nil;case "values":value["values"]=make([]float64,19);case "coverage":value["coverage_bins"]=make([]bool,511);case "null-warning":value["warnings"]=nil;case "warning":value["warnings"]=[]string{"has space"};case "confidence":value["mask_confidence"]=2;case "unknown":value["extra"]=true;case "source":value["source_image"]=ArtifactRef{}}
  raw,err:=canonical.Marshal(value);if err!=nil{t.Fatal(err)}
  stats,err:=ParseStats20(raw)
  if mode=="valid"||mode=="legacy"{if err!=nil||len(stats.Values)!=20{t.Fatal(stats,err)}}else if err==nil{t.Fatal("invalid statistics accepted")}
 })}
}

func TestStatsParamsRequireExplicitPersonAndExactSource(t *testing.T){
 for _,mode:=range []string{"valid","selector","source","canvas","chain","unknown"}{t.Run(mode,func(t *testing.T){
  proposal,_,_,_,protection,_:=repairFixture(t)
  rawMask,err:=canonical.Marshal(protection);if err!=nil{t.Fatal(err)}
  mask,err:=ParseMaskRef(rawMask);if err!=nil{t.Fatal(err)};mask.Selector="person"
  if mode=="selector"{mask.Selector="skin"}
  value:=map[string]any{"image":proposal.document.Image,"person":mask}
  if mode=="unknown"{value["extra"]=true}
  raw,err:=canonical.Marshal(value);if err!=nil{t.Fatal(err)}
  params,err:=ParseStatsParams(raw)
  if mode=="selector"||mode=="unknown"{if err==nil{t.Fatal("invalid Params accepted")};return};if err!=nil{t.Fatal(err)}
  image:=WorkingImage{Canvas:proposal.document.Canvas,TransformChain:proposal.document.TransformChain}
  source:=proposal.document.Image
  switch mode{case "source":source.ID=uuid.New();case "canvas":image.Canvas.Width++;case "chain":image.TransformChain=[]byte(`{}`)}
  err=params.ValidateSource(image,source)
  if mode=="valid"{if err!=nil{t.Fatal(err)}}else if err==nil{t.Fatal("foreign source accepted")}
 })}
}
