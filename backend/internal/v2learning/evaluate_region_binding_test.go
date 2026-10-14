package v2learning

import (
	"testing"
 "math"

	"photoeditor/backend/internal/review"
)

func TestFrozenRegionBindingsRequireExactStyleRuleAndQualifiedMask(t *testing.T) {
	style := []byte(`{"extensions":{"region_adjustments":[{"region_id":"sky","selector":"sky"}]}}`)
	bindings := []TrainingRegionMask{{RegionID: "sky"}}
	masks := map[string]review.MaskRef{"sky": {Selector: "sky", Confidence: .75}}
	if err := validateFrozenRegionBindings(style, bindings, masks); err != nil {
		t.Fatal(err)
	}
	for _, altered := range []map[string]review.MaskRef{
		{},
		{"sky": {Selector: "manual", Confidence: 1}},
		{"sky": {Selector: "sky", Confidence: .749}},
	} {
		if validateFrozenRegionBindings(style, bindings, altered) == nil {
			t.Fatal("invalid region binding accepted")
		}
	}
	if validateFrozenRegionBindings([]byte(`{}`), bindings, masks) == nil ||
		validateFrozenRegionBindings(style, nil, nil) == nil {
		t.Fatal("missing rule or missing mask accepted")
	}
}

func TestFrozenRegionBindingsRejectForeignIdentityAndInvalidConfidence(t *testing.T){
 style:=[]byte(`{"extensions":{"region_adjustments":[{"region_id":"sky","selector":"sky"}]}}`)
 for _,mode:=range []string{"foreign_id","nan","positive_infinity","negative_infinity","above_one","protected"}{t.Run(mode,func(t *testing.T){
  bindings:=[]TrainingRegionMask{{RegionID:"sky"}}
  mask:=review.MaskRef{Selector:"sky",Confidence:1}
  switch mode{
  case "foreign_id":bindings[0].RegionID="other"
  case "nan":mask.Confidence=math.NaN()
  case "positive_infinity":mask.Confidence=math.Inf(1)
  case "negative_infinity":mask.Confidence=math.Inf(-1)
  case "above_one":mask.Confidence=1.01
  case "protected":mask.Protected=true
  }
  if validateFrozenRegionBindings(style,bindings,map[string]review.MaskRef{"sky":mask})==nil{t.Fatal("invalid regional evidence accepted")}
 })}
 duplicateStyle:=[]byte(`{"extensions":{"region_adjustments":[{"region_id":"sky","selector":"sky"},{"region_id":"other","selector":"manual"}]}}`)
 masks:=map[string]review.MaskRef{"sky":{Selector:"sky",Confidence:1},"other":{Selector:"manual",Confidence:1}}
 if validateFrozenRegionBindings(duplicateStyle,[]TrainingRegionMask{{RegionID:"sky"},{RegionID:"sky"}},masks)==nil{t.Fatal("duplicate manifest region accepted")}
}
