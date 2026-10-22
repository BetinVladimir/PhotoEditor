package workflow

import (
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/review"
)

func TestApplyLocalRegionsBindExactPersonAndBackgroundComplement(t *testing.T) {
 image:=review.WorkingImage{Canvas:review.Canvas{Width:32,Height:24},TransformChain:json.RawMessage(`{"steps":[]}`)}
 person:=review.MaskRef{Artifact:review.ArtifactRef{ID:uuid.New(),SHA256:"a"},Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:.9}
 rules:=[]applyLocalRegionRule{{"p","person"},{"b","background"}}
 bindings,err:=bindApplyLocalRegions(rules,&person,nil,image)
 if err!=nil || len(bindings)!=2 || bindings[0].Invert || !bindings[1].Invert || bindings[0].Mask.Artifact!=bindings[1].Mask.Artifact { t.Fatalf("wrong source/complement: %v %v",bindings,err) }
 if _,err=bindApplyLocalRegions(rules,nil,nil,image);err==nil { t.Fatal("missing person mask accepted") }
 person.Protected=true
 if _,err=bindApplyLocalRegions(rules,&person,nil,image);err==nil { t.Fatal("protected mask inverted") }
}

func TestApplyLocalRegionsRefuseUnapprovedForeignGeometry(t *testing.T) {
 image:=review.WorkingImage{Canvas:review.Canvas{Width:32,Height:24},TransformChain:json.RawMessage(`{"steps":[]}`)}
 mask:=review.MaskRef{Artifact:review.ArtifactRef{ID:uuid.New()},Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"sky",Confidence:1}
 binding:=applyOverrideMaskBinding{BindingID:"sky-1",Purpose:"region_adjustment",Mask:mask,Confirmed:true}
 rules:=[]applyLocalRegionRule{{"sky-1","sky"}}
 if _,err:=bindApplyLocalRegions(rules,nil,[]applyOverrideMaskBinding{binding},image);err!=nil { t.Fatal(err) }
 for _, change:=range []func(*applyOverrideMaskBinding){func(b *applyOverrideMaskBinding){b.Confirmed=false},func(b *applyOverrideMaskBinding){b.BindingID="other"},func(b *applyOverrideMaskBinding){b.Mask.Confidence=.74},func(b *applyOverrideMaskBinding){b.Mask.Canvas.Width++},func(b *applyOverrideMaskBinding){b.Mask.TransformChain=json.RawMessage(`{"steps":[1]}`)}} {
  broken:=binding;change(&broken)
  if _,err:=bindApplyLocalRegions(rules,nil,[]applyOverrideMaskBinding{broken},image);err==nil { t.Fatal("invalid exact binding accepted") }
 }
 plan,err:=CompileApplyV4Plan(json.RawMessage(`{"mode":"fixed","extensions":{"region_adjustments":[{"region_id":"p","selector":"person"}]}}`),1)
 if err!=nil || plan[0].Phase!="mask" { t.Fatalf("regional person did not get mask stage: %v %v",plan,err) }
}


func TestTransformedApplyRegionsRepeatSourceAndTargetProvenance(t *testing.T) {
 source:=review.MaskRef{Artifact:review.ArtifactRef{ID:uuid.New()},Selector:"manual",Confidence:1,Canvas:review.Canvas{Width:32,Height:24},TransformChain:json.RawMessage(`{"steps":[]}`)}
 approved:=[]applyOverrideMaskBinding{{BindingID:"r",Purpose:"region_adjustment",Mask:source,Confirmed:true}}
 target:=review.ArtifactRef{ID:uuid.New()}
 image:=review.WorkingImage{Canvas:source.Canvas,TransformChain:json.RawMessage(`{"steps":[1]}`)}
 transformed:=source;transformed.Artifact.ID=uuid.New();transformed.TransformChain=image.TransformChain
 doc:=map[string]any{"params":map[string]any{"target":target,"regions":[]applyLocalRegionBinding{{ID:"r",Mask:source}}},"regions":[]applyLocalRegionBinding{{ID:"r",Mask:transformed}}}
 raw,err:=json.Marshal(doc);if err!=nil { t.Fatal(err) }
 result,err:=transformedApplyRegionBindings(raw,approved,target,image)
 if err!=nil || result[0].Mask.Artifact!=transformed.Artifact { t.Fatalf("exact transformed mask rejected: %v",err) }
 if _,err:=transformedApplyRegionBindings(raw,approved,review.ArtifactRef{ID:uuid.New()},image);err==nil { t.Fatal("foreign target accepted") }
 wrong:=append([]applyOverrideMaskBinding(nil),approved...);wrong[0].Mask.Confidence=.8
 if _,err:=transformedApplyRegionBindings(raw,wrong,target,image);err==nil { t.Fatal("changed approved source accepted") }
 doc["regions"]=[]applyLocalRegionBinding{{ID:"r",Mask:transformed},{ID:"r",Mask:transformed}}
 raw,_=json.Marshal(doc)
 if _,err:=transformedApplyRegionBindings(raw,approved,target,image);err==nil { t.Fatal("duplicate transformed identity accepted") }
}


func TestApplyRegionalTransformResourceBudgetIsSequentialAndIncludesOutputs(t *testing.T) {
 one:=review.MaskRef{Artifact:review.ArtifactRef{ID:uuid.New()}}
 params,_:=json.Marshal(map[string]any{"regions":[]applyLocalRegionBinding{{ID:"r1",Mask:one},{ID:"r2",Mask:one}}})
 raw,err:=applyRegionTransformProfile(1000,128,params);if err!=nil { t.Fatal(err) }
 var profile struct { Estimate ResourceEstimate `json:"resource_estimate"` }
 if json.Unmarshal(raw,&profile)!=nil { t.Fatal("invalid profile") }
 if profile.Estimate.RAMBytes!=3<<30 || profile.Estimate.OutputBytes!=2*(1000*8+1048576)+1048576 || profile.Estimate.ScratchBytes<1000*96 { t.Fatalf("incorrect batch budget: %v",profile) }
 regions:=make([]applyLocalRegionBinding,63)
 for i:=range regions { regions[i].Mask.Artifact.ID=uuid.New() }
 params,_=json.Marshal(map[string]any{"regions":regions})
 if _,err:=applyRegionTransformProfile(1000,128,params);err==nil { t.Fatal("oversized output inventory accepted") }
}

func TestTransformedPortraitProtectionCannotLoseRole(t *testing.T) {
 source:=review.MaskRef{Artifact:review.ArtifactRef{ID:uuid.New()},Selector:"manual",Confidence:.4,Protected:true,Canvas:review.Canvas{Width:32,Height:24},TransformChain:json.RawMessage(`{"steps":[]}`)}
 approved:=[]applyOverrideMaskBinding{{BindingID:"protect",Purpose:"protected",Mask:source}}
 target:=review.ArtifactRef{ID:uuid.New()}
 image:=review.WorkingImage{Canvas:source.Canvas,TransformChain:json.RawMessage(`{"steps":[1]}`)}
 transformed:=source;transformed.Artifact.ID=uuid.New();transformed.TransformChain=image.TransformChain
 doc:=map[string]any{"params":map[string]any{"target":target,"regions":[]applyLocalRegionBinding{{ID:"protect",Mask:source,Purpose:"protected"}}},"regions":[]applyLocalRegionBinding{{ID:"protect",Mask:transformed,Purpose:"protected"}}}
 raw,_:=json.Marshal(doc)
 result,err:=transformedApplyRegionBindings(raw,approved,target,image)
 if err!=nil || !result[0].Mask.Protected || result[0].Mask.Confidence!=.4 { t.Fatalf("protected exact binding lost: %v",err) }
 transformed.Protected=false
 doc["regions"]=[]applyLocalRegionBinding{{ID:"protect",Mask:transformed,Purpose:"protected"}}
 raw,_=json.Marshal(doc)
 if _,err:=transformedApplyRegionBindings(raw,approved,target,image);err==nil { t.Fatal("protection loss accepted") }
}
