package workflow

import (
 "encoding/json"
 "math"
 "strings"
 "testing"
)

func TestCompileIngestFreezesRAWDevelopmentAndPreservesLegacy(t *testing.T) {
 spec,registry:=ingestFixture(t)
 legacy,err:=CompileIngest(spec,registry)
 if err!=nil {t.Fatal(err)}
 for _,node:=range legacy.Nodes {if strings.Contains(string(node.Params),"raw_development"){t.Fatal("legacy params changed")}}
 settings:=RAWDevelopment{1,"dcb","camera",1.5,"blend","rawpy-0.27.1/libraw-0.21.4"}
 spec.Items[1].RAWDevelopment=&settings
 plan,err:=CompileIngest(spec,registry)
 if err!=nil {t.Fatal(err)}
 found:=false
 for _,node:=range plan.Nodes {
  if node.Operation!="asset.decode" || !strings.HasSuffix(node.Key,"0000000000000002"){continue}
  var params ingestDecodeParams
  if err=json.Unmarshal(node.Params,&params);err!=nil{t.Fatal(err)}
  if params.RAWDevelopment==nil || *params.RAWDevelopment!=settings {t.Fatal("RAW settings lost")}
  before:=string(node.Params)
  settings.ExposureEV=2
  if string(node.Params)!=before{t.Fatal("mutable params")}
  found=true
 }
 if !found {t.Fatal("RAW decode missing")}
}

func TestCompileIngestRejectsInvalidRAWDevelopment(t *testing.T) {
 for _,mutate:=range []func(*RAWDevelopment){
  func(p *RAWDevelopment){p.SchemaVersion=0},func(p *RAWDevelopment){p.Demosaic="guess"},
  func(p *RAWDevelopment){p.WBSource="guess"},func(p *RAWDevelopment){p.ExposureEV=math.NaN()},
  func(p *RAWDevelopment){p.ExposureEV=4},func(p *RAWDevelopment){p.HighlightPolicy="guess"},
  func(p *RAWDevelopment){p.DecoderVersion="unlocked"},
 } {
  spec,registry:=ingestFixture(t)
  settings:=RAWDevelopment{1,"ahd","camera",0,"clip","rawpy-0.27.1/libraw-0.21.4"}
  mutate(&settings);spec.Items[1].RAWDevelopment=&settings
  if _,err:=CompileIngest(spec,registry);err==nil{t.Fatal("invalid RAW settings accepted")}
 }
 spec,registry:=ingestFixture(t)
 spec.Items[0].RAWDevelopment=&RAWDevelopment{1,"ahd","camera",0,"clip","rawpy-0.27.1/libraw-0.21.4"}
 if _,err:=CompileIngest(spec,registry);err==nil{t.Fatal("RAW settings on raster accepted")}
}
