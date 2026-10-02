package v2learning

import (
	"testing"

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
