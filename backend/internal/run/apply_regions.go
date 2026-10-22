package run

import (
 "encoding/json"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/review"
)

type applyRegionRule struct {
 ID string `json:"region_id"`
 Selector string `json:"selector"`
 EV float64 `json:"ev"`
 Saturation float64 `json:"saturation"`
 Hue float64 `json:"hue_deg"`
 Strength float64 `json:"strength"`
 Priority int `json:"priority"`
}

func applyRegionsNeedPerson(rules []applyRegionRule) bool {
 for _, r := range rules { if r.Selector == "person" || r.Selector == "background" { return true } }
 return false
}
func applyRegionsNeedExplicitMasks(rules []applyRegionRule) bool {
 for _, r := range rules { if r.Selector != "person" && r.Selector != "background" { return true } }
 return false
}

func validateApplyRegionBindings(rules []applyRegionRule, raw json.RawMessage, geometry bool) error {
 if len(rules) == 0 { return nil }
 encoded, err := json.Marshal(rules)
 if err != nil || !validApplyJobExtension("region_adjustments", encoded) { return domain.NewError("VALIDATION_FAILED", 422) }
 var override struct { Masks []struct {
  ID string `json:"binding_id"`
  Purpose string `json:"purpose"`
  Mask review.MaskRef `json:"mask"`
  Confirmed bool `json:"confirmed"`
 } `json:"mask_refs"` }
 if len(raw)>0 && json.Unmarshal(raw,&override)!=nil { return domain.NewError("VALIDATION_FAILED",422) }
 seen := map[string]bool{}
 distinctMasks := map[string]bool{}
 for _, rule := range rules {
  if rule.ID=="" || len([]rune(rule.ID))>120 || seen[rule.ID] { return domain.NewError("VALIDATION_FAILED",422) };seen[rule.ID]=true
  if rule.Selector=="person" || rule.Selector=="background" { continue }
  count:=0
  for _, binding:= range override.Masks {
   if binding.ID!=rule.ID || binding.Purpose!="region_adjustment" { continue }
   if !binding.Confirmed || binding.Mask.Protected || binding.Mask.Selector!=rule.Selector || binding.Mask.Confidence<.75 { return domain.NewError("OVERRIDE_MASK_INVALID",409) }
   distinctMasks[binding.Mask.Artifact.ID.String()]=true
   count++
  }
  if count!=1 { return domain.NewError("REGION_MASK_REQUIRED",409) }
 }
 if geometry && len(distinctMasks)>62 { return domain.NewError("REGION_MASK_LIMIT_EXCEEDED",422) }
 if len(distinctMasks)>120 { return domain.NewError("REGION_MASK_LIMIT_EXCEEDED",422) }
 return nil
}

// All authored masks carried through body geometry share the single batch budget.
func validateApplyBodyMaskBudget(raw json.RawMessage) error {
 var override struct { Masks []struct { Mask review.MaskRef `json:"mask"` } `json:"mask_refs"` }
 if len(raw)>0 && json.Unmarshal(raw,&override)!=nil { return domain.NewError("VALIDATION_FAILED",422) }
 distinct:=map[review.ArtifactRef]bool{}
 for _, binding:=range override.Masks { distinct[binding.Mask.Artifact]=true }
 if len(distinct)>62 { return domain.NewError("REGION_MASK_LIMIT_EXCEEDED",422) }
 return nil
}
