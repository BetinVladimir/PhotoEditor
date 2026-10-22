package styleplan

import ("encoding/json";"testing")
func phases(plan []Stage)map[string]int{out:=map[string]int{};for i,s:=range plan{out[s.Phase]=i};return out}
func TestStylePlanPortraitDetailUsesApprovedRegionsWithoutInventedFaceModels(t *testing.T){
 plan,e:=Compile(json.RawMessage(`{"mode":"fixed","extensions":{"portrait_detail":{"enabled":true}}}`),1);if e!=nil{t.Fatal(e)};p:=phases(plan)
 if _,ok:=p["portrait_detail"];!ok{t.Fatal("portrait effect missing")};for _,name:=range []string{"mask","face_detect","face_landmarks","face_parse","face_skin"}{if _,ok:=p[name];ok{t.Fatal("unrequested model dependency",name)}}
}
func TestStylePlanCanonicalExtensionCullingAndReviewBarrier(t *testing.T){
 plan,e:=Compile(json.RawMessage(`{"mode":"fixed","extensions":{"culling":{"enabled":true}}}`),1);if e!=nil{t.Fatal(e)};p:=phases(plan)
 if _,ok:=p["cull_review"];!ok{t.Fatal("StyleV2 extension culling was ignored")}
 if p["cull_review"]>=p["color"]{t.Fatal("selection barrier must precede effect work")}
 for _,s:=range plan{if s.Phase=="cull_review"&&(s.Cardinality!="review"||s.Operation!=""){t.Fatal("review replaced with automatic effect")}}
}
func TestStylePlanRejectsUnknownDustPolicy(t *testing.T){if _,e:=Compile(json.RawMessage(`{"mode":"fixed","extensions":{"restoration":{"sensor_dust":"erase_all"}}}`),1);e==nil{t.Fatal("unknown dust policy admitted")}}
func TestStylePlanRetainsOrderedReviewAndGeometry(t *testing.T){
 plan,e:=Compile(json.RawMessage(`{"mode":"fixed","geometry":{"enabled":true},"defects":{"enabled":true,"policy":"review"},"extensions":{"generation":{"enabled":true},"composition":{"enabled":true},"restoration":{"denoise":"scunet_color_real_psnr","upscale":"2x"}}}`),1);if e!=nil{t.Fatal(e)};p:=phases(plan)
 order:=[]string{"denoise","geometry","mask_transform","color","defects_propose","defects_review","generate","generation_review","composition_render","composition_review","upscale","quality"};for i:=1;i<len(order);i++{a,aok:=p[order[i-1]];b,bok:=p[order[i]];if !aok||!bok||a>=b{t.Fatal("effect or review order changed",order[i-1],order[i])}}
}
