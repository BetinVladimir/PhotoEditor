package workflow

import (
 "bytes"
 "encoding/json"
 "testing"
 "photoeditor/backend/internal/canonical"
)

func TestAuthoredBaseFullModulesAndHumanBarriersRetained(t *testing.T) {
 raw:=json.RawMessage(`{"mode":"adaptive","local":{"enabled":true},"geometry":{"enabled":true},"beauty":{"enabled":true},"defects":{"enabled":true,"policy":"review"},"extensions":{"culling":{"enabled":true},"series":{"enabled":true},"portrait_detail":{"enabled":true},"restoration":{"denoise":"scunet_color_real_psnr","upscale":"2x","sensor_dust":"review"},"background":{"mode":"generate"},"inpainting":{"enabled":true},"generation":{"enabled":true},"composition":{"enabled":true}}}`)
 before:=append([]byte(nil),raw...)
 result,err:=CompileAuthoredBasePlan(raw,2)
 if err!=nil{t.Fatal(err)}
 expected:=[]string{"mask","stats","resolve","cull_embedding","cull_features","cull_pack","cull_group","cull_rank","cull_review","denoise","pose","geometry","mask_transform","color","series_measure","series_pack","series_balance","dust_detect","dust_review","dust_repair","local","face_detect","face_landmarks","face_parse","face_skin","beauty","portrait_detail","defects_propose","defects_review","background","background_review","inpaint","inpaint_review","generate","generation_review","composition_structure","composition","composition_render","composition_review","upscale","quality"}
 if len(result.Stages)!=len(expected){t.Fatalf("stage count %d",len(result.Stages))}
 for i,name:=range expected {
  stage:=result.Stages[i]
  if stage.Phase!=name{t.Fatalf("stage %d %s, want %s",i,stage.Phase,name)}
  if stage.Cardinality=="review"&&(stage.Operation!=""||stage.OutputRole!=""){t.Fatal("review barrier became worker operation")}
 }
 if !bytes.Equal(raw,before){t.Fatal("effective style mutated")}
 body:=struct{Style json.RawMessage `json:"effective_style"`;Stages []ApplyV4Stage `json:"stages"`}{result.EffectiveStyle,result.Stages}
 digest,err:=canonical.Hash(body);if err!=nil||digest!=result.SHA256{t.Fatal("plan binding")}
 replay,err:=CompileAuthoredBasePlan(raw,2);if err!=nil||replay.SHA256!=result.SHA256{t.Fatal("unstable replay")}
}

func TestAuthoredBaseInvalidStyleOrCardinalityRefused(t *testing.T){
 for _,test:=range []struct{raw string;count int}{{`null`,1},{`{"mode":"unknown"}`,1},{`{"mode":"fixed"}`,0},{`{"mode":"fixed"}`,5001},{`{"mode":"fixed","defects":{"enabled":true,"policy":"ignore"}}`,1}} {
  if _,err:=CompileAuthoredBasePlan(json.RawMessage(test.raw),test.count);err==nil{t.Fatal("invalid plan admitted")}
 }
}
