package run

import (
 "encoding/json"
 "testing"
)

func TestApplyRegionsRequirePersonOrExplicitApprovedBinding(t *testing.T) {
 person:=[]applyRegionRule{{ID:"p",Selector:"person",Saturation:1,Strength:1},{ID:"b",Selector:"background",Saturation:1,Strength:1}}
 if !applyRegionsNeedPerson(person) || applyRegionsNeedExplicitMasks(person) || validateApplyRegionBindings(person,nil,true)!=nil { t.Fatal("automatic person/background rejected") }
 manual:=[]applyRegionRule{{ID:"m",Selector:"manual",Saturation:1,Strength:1}}
 valid:=json.RawMessage(`{"mask_refs":[{"binding_id":"m","purpose":"region_adjustment","confirmed":true,"mask":{"selector":"manual","confidence":0.75,"protected":false}}]}`)
 if applyRegionsNeedPerson(manual) || !applyRegionsNeedExplicitMasks(manual) || validateApplyRegionBindings(manual,valid,false)!=nil { t.Fatal("approved manual binding rejected") }
 if validateApplyRegionBindings(manual,valid,true)!=nil { t.Fatal("approved manual mask refused before exact body transform") }
 for _, raw:=range []string{`{}`,`{"mask_refs":[{"binding_id":"m","purpose":"region_adjustment","confirmed":false,"mask":{"selector":"manual","confidence":1}}]}`,`{"mask_refs":[{"binding_id":"m","purpose":"inpaint_target","confirmed":true,"mask":{"selector":"manual","confidence":1}}]}`,`{"mask_refs":[{"binding_id":"m","purpose":"region_adjustment","confirmed":true,"mask":{"selector":"sky","confidence":1}}]}`,`{"mask_refs":[{"binding_id":"m","purpose":"region_adjustment","confirmed":true,"mask":{"selector":"manual","confidence":0.74}}]}`,`{"mask_refs":[{"binding_id":"m","purpose":"region_adjustment","confirmed":true,"mask":{"selector":"manual","confidence":1,"protected":true}}]}`} {
  if validateApplyRegionBindings(manual,json.RawMessage(raw),false)==nil { t.Fatalf("invalid approval accepted: %s",raw) }
 }
 if validateApplyRegionBindings(append(person,person[0]),nil,false)==nil { t.Fatal("duplicate region accepted") }
 var needs styleExecutionNeeds
 if err:=json.Unmarshal([]byte(`{"extensions":{"region_adjustments":[{"region_id":"old","selector":"person","saturation":1,"strength":1}]}}`),&needs);err!=nil { t.Fatal(err) }
 if err:=mergeOverrideNeeds(&needs,json.RawMessage(`{"modules":{"extensions":{"region_adjustments":[]}}}`));err!=nil { t.Fatal(err) }
 if len(needs.Extensions.RegionAdjustments)!=0 { t.Fatal("region replacement retained old rules") }
}
