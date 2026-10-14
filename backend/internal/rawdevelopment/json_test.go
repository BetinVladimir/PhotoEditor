package rawdevelopment

import (
 "encoding/json"
 "strings"
 "testing"
)

func TestPolicyJSONRequiresCompleteCanonicalShape(t *testing.T) {
 raw:=`{"schema_version":1,"demosaic":"ahd","wb_source":"camera","exposure_ev":0,"highlight_policy":"clip","decoder_version":"rawpy-0.27.1/libraw-0.21.4"}`
 var p Policy
 if err:=json.Unmarshal([]byte(raw),&p);err!=nil || !Valid(&p){t.Fatal(err)}
 for _,bad:=range []string{
  strings.Replace(raw,`"exposure_ev":0,`,"",1),
  strings.Replace(raw,`"exposure_ev":0`,`"exposure_ev":null`,1),
  strings.Replace(raw,`"wb_source":"camera"`,`"wb_source":null`,1),
  strings.Replace(raw,`"schema_version":1`,`"schema_version":1,"schema_version":1`,1),
  strings.Replace(raw,`"schema_version":1`,`"schema_version":1,"unknown":0`,1),
 } {
  if err:=json.Unmarshal([]byte(bad),&p);err==nil {t.Fatal("incomplete policy accepted")}
 }
}
