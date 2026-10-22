package editor

import (
 "encoding/json"
 "strings"
 "testing"
)

func TestOverrideUsesSharedStrictModuleContracts(t *testing.T){
 source:=strings.Repeat("a",64)
 for _,modules:=range []string{`{"strength":0}`,`{"local":{"enabled":false}}`,`{"beauty":{"enabled":false}}`,`{"extensions":{"restoration":{"denoise":"off","denoise_strength":0,"upscale":"off","sensor_dust":"off"}}}`} {
  raw:=json.RawMessage(`{"schema_version":1,"input_sha256":"`+source+`","revision":1,"modules":`+modules+`}`)
  if err:=ValidateOverride(raw,source);err!=nil{t.Fatalf("valid module rejected %s: %v",modules,err)}
 }
 for _,modules:=range []string{`{"strength":null}`,`{"strength":1.01}`,`{"strength":"1"}`,`{"global":{}}`,`{"local":{"enabled":true}}`,`{"beauty":{"enabled":false,"smoothing":1}}`,`{"geometry":{"enabled":true}}`,`{"extensions":{"generation":{"enabled":true}}}`,`{"extensions":{"unknown":{}}}`} {
  raw:=json.RawMessage(`{"schema_version":1,"input_sha256":"`+source+`","revision":1,"modules":`+modules+`}`)
  if ValidateOverride(raw,source)==nil{t.Fatalf("invalid complete module accepted: %s",modules)}
 }
}
