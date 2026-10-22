package workflow

import (
 "bytes"
 "encoding/json"
 "errors"
 "github.com/google/uuid"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/canonical"
)

type applyLocalRegionRule struct {
 ID string `json:"region_id"`
 Selector string `json:"selector"`
}
type applyLocalRegionBinding struct {
 ID string `json:"region_id"`
 Mask review.MaskRef `json:"mask"`
 Invert bool `json:"invert,omitempty"`
 Purpose string `json:"purpose,omitempty"`
}
func resolvedApplyRegionRules(raw []byte) ([]applyLocalRegionRule,error) {
 var resolved struct { Modules struct { Extensions struct {
  Regions []applyLocalRegionRule `json:"region_adjustments"`
 } `json:"extensions"` } `json:"modules"` }
 if err:=json.Unmarshal(raw,&resolved); err!=nil { return nil,err }
 return resolved.Modules.Extensions.Regions,nil
}
func bindApplyLocalRegions(rules []applyLocalRegionRule, person *review.MaskRef, masks []applyOverrideMaskBinding, working review.WorkingImage) ([]applyLocalRegionBinding,error) {
 result:=make([]applyLocalRegionBinding,0,len(rules))
 seen:=map[string]bool{}
 if len(rules)>128 { return nil,errors.New("too many regional adjustments") }
 for _, rule:=range rules {
  if rule.ID=="" || seen[rule.ID] { return nil,errors.New("duplicate or empty region identity") };seen[rule.ID]=true
  var mask review.MaskRef
  invert:=false
  if rule.Selector=="person" || rule.Selector=="background" {
   if person==nil { return nil,errors.New("regional person mask missing") }
   mask=*person;invert=rule.Selector=="background"
  } else {
   count:=0
   for _, b:=range masks {
    if b.Purpose=="region_adjustment" && b.BindingID==rule.ID {
     if !b.Confirmed || b.Mask.Selector!=rule.Selector || b.Mask.Confidence<.75 { return nil,errors.New("regional approval mismatch") }
     mask=b.Mask;count++
    }
   }
   if count!=1 { return nil,errors.New("exact regional mask missing") }
  }
  if mask.Artifact.ID==uuid.Nil || mask.Protected || mask.Canvas!=working.Canvas || !bytes.Equal(mask.TransformChain,working.TransformChain) {
   return nil,errors.New("regional mask geometry or protection mismatch")
  }
  result=append(result,applyLocalRegionBinding{ID:rule.ID,Mask:mask,Invert:invert})
 }
 return result,nil
}


func applyStyleRegionRules(raw json.RawMessage) ([]applyLocalRegionRule,error) {
 var style struct { Extensions struct { Regions []applyLocalRegionRule `json:"region_adjustments"` } `json:"extensions"` }
 if err:=json.Unmarshal(raw,&style);err!=nil { return nil,err };return style.Extensions.Regions,nil
}

func explicitApplyRegionSources(rules []applyLocalRegionRule, masks []applyOverrideMaskBinding, source review.MaskRef) ([]applyLocalRegionBinding,error) {
 explicit:=make([]applyLocalRegionRule,0)
 for _, rule:=range rules { if rule.Selector!="person" && rule.Selector!="background" { explicit=append(explicit,rule) } }
 result,err:=bindApplyLocalRegions(explicit,nil,masks,review.WorkingImage{Canvas:source.Canvas,TransformChain:source.TransformChain})
 if err!=nil { return nil,err }
 for _, binding:=range masks {
  if binding.Purpose!="protected" && binding.Purpose!="inpaint_target" && binding.Purpose!="generation_target" && !bytes.HasPrefix([]byte(binding.Purpose),[]byte("portrait_")) { continue }
  if binding.Mask.Canvas!=source.Canvas || !bytes.Equal(binding.Mask.TransformChain,source.TransformChain) { return nil,errors.New("portrait transform source geometry mismatch") }
  result=append(result,applyLocalRegionBinding{ID:binding.BindingID,Mask:binding.Mask,Purpose:binding.Purpose})
 }
 return result,nil
}

func transformedApplyRegionBindings(raw []byte, original []applyOverrideMaskBinding, target review.ArtifactRef, working review.WorkingImage) ([]applyOverrideMaskBinding,error) {
 var root struct {
  Params struct { Target review.ArtifactRef `json:"target"`; Regions []applyLocalRegionBinding `json:"regions"` } `json:"params"`
  Regions []struct { ID string `json:"region_id"`; Mask review.MaskRef `json:"mask"`; Purpose string `json:"purpose"` } `json:"regions"`
 }
 if json.Unmarshal(raw,&root)!=nil || root.Params.Target!=target || len(root.Params.Regions)!=len(root.Regions) { return nil,errors.New("regional transform target/count mismatch") }
 result:=append([]applyOverrideMaskBinding(nil),original...)
 seen:=map[string]bool{}
 for _, transformed:=range root.Regions {
  if seen[transformed.ID] { return nil,errors.New("duplicate transformed region") };seen[transformed.ID]=true
  purpose:=transformed.Purpose;if purpose=="" { purpose="region_adjustment" }
  match:=-1
  for i,binding:=range result { if binding.Purpose==purpose && binding.BindingID==transformed.ID { if match>=0 { return nil,errors.New("duplicate approved region") };match=i } }
  if match<0 { return nil,errors.New("unapproved transformed region") }
  source:=result[match].Mask
  count:=0
  for _, input:=range root.Params.Regions {
   inputPurpose:=input.Purpose;if inputPurpose=="" { inputPurpose="region_adjustment" };if input.ID!=transformed.ID || inputPurpose!=purpose { continue }
   a,_:=json.Marshal(input.Mask);b,_:=json.Marshal(source)
   if !bytes.Equal(a,b) { return nil,errors.New("transformed region source mismatch") };count++
  }
  mask:=transformed.Mask
  objectEqual:=source.ObjectID==nil && mask.ObjectID==nil || source.ObjectID!=nil && mask.ObjectID!=nil && *source.ObjectID==*mask.ObjectID
  lockEqual:=source.ModelLockID==nil && mask.ModelLockID==nil || source.ModelLockID!=nil && mask.ModelLockID!=nil && *source.ModelLockID==*mask.ModelLockID
  if count!=1 || (purpose=="region_adjustment" && !result[match].Confirmed) || mask.Protected!=source.Protected || mask.Selector!=source.Selector || mask.Confidence!=source.Confidence || !objectEqual || !lockEqual || mask.Artifact.ID==uuid.Nil || mask.Canvas!=working.Canvas || !bytes.Equal(mask.TransformChain,working.TransformChain) {
   return nil,errors.New("transformed regional provenance mismatch")
  }
  result[match].Mask=mask
 }
 return result,nil
}


func applyRegionTransformProfile(pixels,inputBytes int64, params json.RawMessage) (json.RawMessage,error) {
 var value struct { Regions []applyLocalRegionBinding `json:"regions"` }
 if json.Unmarshal(params,&value)!=nil || pixels<1 || pixels>200000000 || inputBytes<0 || inputBytes>1048576 { return nil,errors.New("invalid regional transform resource dimensions") }
 unique:=map[review.ArtifactRef]bool{}
 for _, region:=range value.Regions { unique[region.Mask.Artifact]=true }
 if len(unique)>62 { return nil,errors.New("regional transform output budget exceeded") }
 output:=int64(len(unique)+1)*(pixels*8+1048576)+1048576
 scratch:=pixels*96+inputBytes+8*1048576
 return canonical.Marshal(struct {
  Backend string `json:"backend"`; Precision string `json:"precision"`; Tile int `json:"tile_size"`; Estimate ResourceEstimate `json:"resource_estimate"`
 }{"cpu","float32",512,ResourceEstimate{RAMBytes:3<<30,ScratchBytes:scratch,OutputBytes:output,PixelCount:pixels}})
}
