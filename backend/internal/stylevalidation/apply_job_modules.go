package stylevalidation

import (
 "bytes"
 "encoding/json"
 "io"
 "math"
)

// Module mirrors the strict discriminated module contracts in
// photo_worker/contracts/style.py. Admission must refuse incomplete modules;
// replacement overrides are complete module values, never partial patches.
func Module(name string, body json.RawMessage) bool {
 if name != "local" && name != "beauty" && name != "defects" && name != "geometry" { return false }
 m, ok := applyModuleObject(body)
 if !ok { return false }
 var enabled *bool
 if json.Unmarshal(m["enabled"], &enabled) != nil || enabled == nil { return false }
 if !*enabled { return len(m) == 1 }
 number := func(field string, low, high float64, openLow bool) bool {
  value, valid := applyModuleNumber(m[field])
  return valid && value >= low && value <= high && (!openLow || value > low)
 }
 literal := func(field, expected string) bool {
  var value *string
  return json.Unmarshal(m[field], &value) == nil && value != nil && *value == expected
 }
 trueFlag := func(field string) bool {
  var value *bool
  return json.Unmarshal(m[field], &value) == nil && value != nil && *value
 }
 fields := func(names ...string) bool {
  if len(m) != len(names)+1 { return false }
  for _, field := range names { if _, present := m[field]; !present { return false } }
  return true
 }
 switch name {
 case "local":
  return fields("person_ev", "background_ev", "person_saturation", "background_saturation", "mask_policy_id") &&
   number("person_ev", -1, 1, false) && number("background_ev", -1, 1, false) &&
   number("person_saturation", .7, 1.3, false) && number("background_saturation", .7, 1.3, false) &&
   literal("mask_policy_id", "person_guided_v1")
 case "beauty":
  return fields("algorithm", "smoothing", "tone_evenness", "texture_keep", "skin_mask", "protect_features") &&
   literal("algorithm", "beautyfilter_v1") && number("smoothing", 0, 1, false) &&
   number("tone_evenness", 0, 1, false) && number("texture_keep", 0, 1, false) &&
   literal("skin_mask", "face68_conservative_v1") && trueFlag("protect_features")
 case "defects":
  if !fields("algorithm", "policy", "strength", "diameter_face_fraction", "preserve_permanent_marks") ||
   !literal("algorithm", "defectrepair_telea_v1") ||
   !(literal("policy", "review") || literal("policy", "conservative_auto")) ||
   !number("strength", 0, 1, false) || !trueFlag("preserve_permanent_marks") { return false }
  var diameter []json.RawMessage
  if json.Unmarshal(m["diameter_face_fraction"], &diameter) != nil || len(diameter) != 2 { return false }
  low, lowOK := applyModuleNumber(diameter[0])
  high, highOK := applyModuleNumber(diameter[1])
  return lowOK && highOK && .002 <= low && low <= high && high <= .012
 case "geometry":
  return fields("algorithm", "body_strength", "face_slim", "waist_slim", "hip_slim", "torso_slim", "leg_length", "protect_background", "max_displacement_image_fraction", "on_unsafe") &&
   literal("algorithm", "bodywarp_v1") && number("body_strength", 0, 1, false) &&
   number("face_slim", 0, .05, false) && number("waist_slim", 0, .08, false) &&
   number("hip_slim", 0, .08, false) && number("torso_slim", 0, .08, false) &&
   number("leg_length", 0, .03, false) && trueFlag("protect_background") &&
   number("max_displacement_image_fraction", 0, .02, true) && literal("on_unsafe", "skip_module")
 }
 return false
}

// Decode top-level members individually to reject duplicate keys before a map
// can overwrite them. Module contracts have no nested objects.
func applyModuleObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
 if len(raw) == 0 || len(raw) > 1048576 { return nil, false }
 decoder := json.NewDecoder(bytes.NewReader(raw))
 start, err := decoder.Token()
 if err != nil || start != json.Delim('{') { return nil, false }
 result := map[string]json.RawMessage{}
 for decoder.More() {
  token, err := decoder.Token()
  key, valid := token.(string)
  if err != nil || !valid { return nil, false }
  if _, exists := result[key]; exists { return nil, false }
  var value json.RawMessage
  if decoder.Decode(&value) != nil { return nil, false }
  result[key] = value
 }
 end, err := decoder.Token()
 if err != nil || end != json.Delim('}') || decoder.Decode(new(any)) != io.EOF { return nil, false }
 return result, true
}

func applyModuleNumber(raw json.RawMessage) (float64, bool) {
 var value *float64
 if json.Unmarshal(raw, &value) != nil || value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) { return 0, false }
 return *value, true
}
