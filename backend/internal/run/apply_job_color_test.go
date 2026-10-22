package run

import (
 "encoding/json"
 "strings"
 "testing"
)

func applyJobColorFixture() map[string]any {
 curve:=[]float64{0,.1,.2,.5,.8,.9,1}
 return map[string]any{"gains_rgb":[]float64{.25,1,4},"master_curve":curve,"rgb_curves":[][]float64{curve,curve,curve},"saturation":2,"residual_lut":map[string]any{"kind":"residual","size":17,"encoding":"constant","values":[]float64{-.25,0,.25}}}
}
func TestValidApplyJobColorDefaultsAndDense(t *testing.T){
 color:=applyJobColorFixture();raw,_:=json.Marshal(color);if !validApplyJobColor(raw){t.Fatal("constant LUT with omitted defaults rejected")}
 lut:=color["residual_lut"].(map[string]any);lut["order"]="RGB_C";lut["domain"]=[]float64{0,1};lut["encoding"]="dense";lut["values"]=make([]float64,17*17*17*3)
 raw,_=json.Marshal(color);if !validApplyJobColor(raw){t.Fatal("dense LUT with explicit defaults rejected")}
}
func TestInvalidApplyJobColor(t *testing.T){
 cases:=map[string]func(map[string]any){
  "missing gains":func(c map[string]any){delete(c,"gains_rgb")},
  "unknown field":func(c map[string]any){c["enabled"]=true},
  "null saturation":func(c map[string]any){c["saturation"]=nil},
  "string saturation":func(c map[string]any){c["saturation"]="1"},
  "saturation high":func(c map[string]any){c["saturation"]=2.01},
  "gains low":func(c map[string]any){c["gains_rgb"]=[]float64{.24,1,1}},
  "gains count":func(c map[string]any){c["gains_rgb"]=[]float64{1,1}},
  "null gain":func(c map[string]any){c["gains_rgb"]=[]any{1,nil,1}},
  "curve count":func(c map[string]any){c["master_curve"]=[]float64{0,1}},
  "curve decreasing":func(c map[string]any){c["master_curve"]=[]float64{0,.2,.1,.5,.8,.9,1}},
  "curve out of range":func(c map[string]any){c["master_curve"]=[]float64{0,.1,.2,.5,.8,.9,1.01}},
  "rgb count":func(c map[string]any){c["rgb_curves"]=[]any{}},
  "rgb decreasing":func(c map[string]any){c["rgb_curves"]=[][]float64{{0,.2,.1,.5,.8,.9,1},{0,0,0,0,0,0,0},{1,1,1,1,1,1,1}}},
 }
 for name,change:=range cases{t.Run(name,func(t *testing.T){color:=applyJobColorFixture();change(color);raw,_:=json.Marshal(color);if validApplyJobColor(raw){t.Fatal("invalid color accepted")}})}
 for _,field:=range []string{"master_curve","rgb_curves","saturation","residual_lut"}{t.Run("missing "+field,func(t *testing.T){color:=applyJobColorFixture();delete(color,field);raw,_:=json.Marshal(color);if validApplyJobColor(raw){t.Fatal("missing field accepted")}})}
}
func TestInvalidApplyJobResidualLUT(t *testing.T){
 cases:=map[string]any{"kind":"absolute","size":33,"order":"BGR_C","domain":[]float64{-1,1},"encoding":"packed","values":[]float64{0,0,.251},"extra":true}
 for field,value:=range cases{t.Run(field,func(t *testing.T){color:=applyJobColorFixture();color["residual_lut"].(map[string]any)[field]=value;raw,_:=json.Marshal(color);if validApplyJobColor(raw){t.Fatal("invalid LUT accepted")}})}
 for _,field:=range []string{"kind","size","encoding","values"}{t.Run("missing "+field,func(t *testing.T){color:=applyJobColorFixture();delete(color["residual_lut"].(map[string]any),field);raw,_:=json.Marshal(color);if validApplyJobColor(raw){t.Fatal("missing LUT field accepted")}})}
 for _,field:=range []string{"order","domain","values","size","kind","encoding"}{t.Run("null "+field,func(t *testing.T){color:=applyJobColorFixture();color["residual_lut"].(map[string]any)[field]=nil;raw,_:=json.Marshal(color);if validApplyJobColor(raw){t.Fatal("null LUT field accepted")}})}
 color:=applyJobColorFixture();lut:=color["residual_lut"].(map[string]any)
 for _,count:=range []int{0,2,4}{lut["encoding"]="constant";lut["values"]=make([]float64,count);raw,_:=json.Marshal(color);if validApplyJobColor(raw){t.Fatalf("constant count %d accepted",count)}}
 for _,count:=range []int{0,2,4,17*17*17*3-1,17*17*17*3+1}{lut["encoding"]="dense";lut["values"]=make([]float64,count);raw,_:=json.Marshal(color);if validApplyJobColor(raw){t.Fatalf("dense count %d accepted",count)}}
}
func TestApplyJobColorStrictJSON(t *testing.T){
 raw,_:=json.Marshal(applyJobColorFixture());text:=string(raw)
 for _,invalid:=range []string{"null","[]",text+" {}",strings.Replace(text,"{","{\"saturation\":1,",1),strings.Replace(text,"\"kind\":\"residual\"","\"kind\":\"residual\",\"kind\":\"residual\"",1),strings.Replace(text,"\"saturation\":2","\"saturation\":1e10000",1)}{if validApplyJobColor(json.RawMessage(invalid)){t.Fatal("invalid JSON shape accepted")}}
}

func TestApplyJobColorLUTSizeIsStrictInteger(t *testing.T) {
 color:=applyJobColorFixture()
 raw,err:=json.Marshal(color)
 if err!=nil {t.Fatal(err)}
 for _,value:=range []string{"17.0","1.7e1","true","null","\"17\""} {
  changed:=strings.Replace(string(raw),`"size":17`,`"size":`+value,1)
  if changed==string(raw) {t.Fatal("fixture size missing")}
  if validApplyJobColor(json.RawMessage(changed)) {t.Fatalf("noninteger LUT size accepted: %s",value)}
 }
}
