package run

import (
 "encoding/json"
 "testing"
)

func applyModuleExamples() map[string]string {
 return map[string]string{
  "local": `{"enabled":true,"person_ev":0,"background_ev":0,"person_saturation":1,"background_saturation":1,"mask_policy_id":"person_guided_v1"}`,
  "beauty": `{"enabled":true,"algorithm":"beautyfilter_v1","smoothing":0.5,"tone_evenness":0.5,"texture_keep":0.5,"skin_mask":"face68_conservative_v1","protect_features":true}`,
  "defects": `{"enabled":true,"algorithm":"defectrepair_telea_v1","policy":"review","strength":0.5,"diameter_face_fraction":[0.002,0.012],"preserve_permanent_marks":true}`,
  "geometry": `{"enabled":true,"algorithm":"bodywarp_v1","body_strength":0.5,"face_slim":0.02,"waist_slim":0.04,"hip_slim":0.04,"torso_slim":0.04,"leg_length":0.01,"protect_background":true,"max_displacement_image_fraction":0.01,"on_unsafe":"skip_module"}`,
 }
}

func changedApplyModule(t *testing.T, original, field, value string) json.RawMessage {
 t.Helper()
 var fields map[string]json.RawMessage
 if err := json.Unmarshal([]byte(original), &fields); err != nil { t.Fatal(err) }
 if value == "" { delete(fields, field) } else { fields[field] = json.RawMessage(value) }
 raw, err := json.Marshal(fields)
 if err != nil { t.Fatal(err) }
 return raw
}

func TestApplyJobModuleRequiredClosedStrictContracts(t *testing.T) {
 for name, valid := range applyModuleExamples() {
  t.Run(name, func(t *testing.T) {
   if !validApplyJobModule(name, json.RawMessage(valid)) { t.Fatal("valid complete module rejected") }
   if !validApplyJobModule(name, json.RawMessage(`{"enabled":false}`)) { t.Fatal("disabled module rejected") }
   for _, raw := range []string{`null`, `[]`, `{}`, `{"enabled":null}`, `{"enabled":0}`, `{"enabled":"true"}`, `{"enabled":true}`, `{"enabled":false,"unused":null}`, valid+`{}`, `{"enabled":false,"enabled":false}`, `{"enabled":false,"enab\u006ced":false}`} {
    if validApplyJobModule(name, json.RawMessage(raw)) { t.Fatalf("invalid value accepted: %s", raw) }
   }
   var fields map[string]json.RawMessage
   if err := json.Unmarshal([]byte(valid), &fields); err != nil { t.Fatal(err) }
   for field := range fields {
    if validApplyJobModule(name, changedApplyModule(t,valid,field,"")) { t.Fatalf("missing %s accepted",field) }
    if validApplyJobModule(name, changedApplyModule(t,valid,field,"null")) { t.Fatalf("null %s accepted",field) }
   }
   if validApplyJobModule(name, changedApplyModule(t,valid,"unknown","0")) { t.Fatal("unknown field accepted") }
  })
 }
 if validApplyJobModule("global",json.RawMessage(`{"enabled":false}`)) || validApplyJobModule("",json.RawMessage(`{"enabled":false}`)) { t.Fatal("unsupported module accepted") }
}

func TestApplyJobModuleNumericBoundsAndTypes(t *testing.T) {
 examples := applyModuleExamples()
 cases := []struct { module, field, low, high, below, above string }{
  {"local","person_ev","-1","1","-1.001","1.001"},
  {"local","background_ev","-1","1","-1.001","1.001"},
  {"local","person_saturation","0.7","1.3","0.699","1.301"},
  {"local","background_saturation","0.7","1.3","0.699","1.301"},
  {"beauty","smoothing","0","1","-0.001","1.001"},
  {"beauty","tone_evenness","0","1","-0.001","1.001"},
  {"beauty","texture_keep","0","1","-0.001","1.001"},
  {"defects","strength","0","1","-0.001","1.001"},
  {"geometry","body_strength","0","1","-0.001","1.001"},
  {"geometry","face_slim","0","0.05","-0.001","0.051"},
  {"geometry","waist_slim","0","0.08","-0.001","0.081"},
  {"geometry","hip_slim","0","0.08","-0.001","0.081"},
  {"geometry","torso_slim","0","0.08","-0.001","0.081"},
  {"geometry","leg_length","0","0.03","-0.001","0.031"},
  {"geometry","max_displacement_image_fraction","0.000001","0.02","0","0.021"},
 }
 for _, c := range cases {
  t.Run(c.module+"/"+c.field, func(t *testing.T) {
   for _, bound := range []string{c.low,c.high} {
    if !validApplyJobModule(c.module,changedApplyModule(t,examples[c.module],c.field,bound)) { t.Fatalf("boundary %s rejected",bound) }
   }
   for _, invalid := range []string{c.below,c.above,`1e1000`,`"0.5"`,`true`,`false`,`[]`,`{}`,`null`} {
    if validApplyJobModule(c.module,changedApplyModule(t,examples[c.module],c.field,invalid)) { t.Fatalf("invalid numeric %s accepted",invalid) }
   }
  })
 }
 for _, invalid := range []string{`{"enabled":true,"person_ev":NaN}`,`{"enabled":true,"person_ev":Infinity}`,`{"enabled":true,"person_ev":1e1000}`} {
  if validApplyJobModule("local",json.RawMessage(invalid)) { t.Fatal("nonfinite number accepted") }
 }
}

func TestApplyJobModuleEnumsProtectionAndDiameterOrder(t *testing.T) {
 examples := applyModuleExamples()
 for _, c := range []struct { module, field, invalid string }{
  {"local","mask_policy_id",`"person"`},
  {"beauty","algorithm",`"other"`}, {"beauty","skin_mask",`"other"`},
  {"beauty","protect_features",`false`}, {"beauty","protect_features",`1`},
  {"defects","algorithm",`"other"`}, {"defects","policy",`"auto"`},
  {"defects","preserve_permanent_marks",`false`}, {"defects","preserve_permanent_marks",`"true"`},
  {"geometry","algorithm",`"other"`}, {"geometry","on_unsafe",`"apply"`},
  {"geometry","protect_background",`false`}, {"geometry","protect_background",`1`},
 } {
  if validApplyJobModule(c.module,changedApplyModule(t,examples[c.module],c.field,c.invalid)) { t.Fatalf("invalid %s.%s accepted",c.module,c.field) }
 }
 for _, diameter := range []string{`[0.002,0.002]`,`[0.012,0.012]`,`[0.004,0.008]`} {
  if !validApplyJobModule("defects",changedApplyModule(t,examples["defects"],"diameter_face_fraction",diameter)) { t.Fatalf("valid diameter rejected: %s",diameter) }
 }
 for _, diameter := range []string{`[]`,`[0.002]`,`[0.002,0.01,0.012]`,`[0.001,0.012]`,`[0.002,0.013]`,`[0.01,0.004]`,`[null,0.01]`,`[0.002,null]`,`[true,0.01]`,`["0.002",0.01]`,`{}`} {
  if validApplyJobModule("defects",changedApplyModule(t,examples["defects"],"diameter_face_fraction",diameter)) { t.Fatalf("invalid diameter accepted: %s",diameter) }
 }
 if !validApplyJobModule("defects",changedApplyModule(t,examples["defects"],"policy",`"conservative_auto"`)) { t.Fatal("conservative_auto rejected") }
}
