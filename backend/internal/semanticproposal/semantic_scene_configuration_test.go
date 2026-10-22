package semanticproposal

import (
 "encoding/json"
 "testing"
 "photoeditor/backend/internal/v2qualification"
)

func TestSceneConfigurationOwnsProducerPoliciesAndRawThresholds(t *testing.T){
 configuration:=TransferPreparation{Scene:&TransferScenePreparation{
  Policy:json.RawMessage(`{"algorithm":"farneback_depth_plane_v1"}`),
  Scene:TransferSceneProducer{Expected:v2qualification.Expected{RequiredChecks:[]string{"scene"},EvidenceSHAs:map[string]struct{}{"scene":{}}}},
  Depth:TransferSceneProducer{Expected:v2qualification.Expected{RequiredChecks:[]string{"depth"},EvidenceSHAs:map[string]struct{}{"depth":{}}}},
 }}
 owned:=ownTransferPreparation(configuration)
 configuration.Scene.Policy[2]='X'
 configuration.Scene.Scene.Expected.RequiredChecks[0]="changed"
 delete(configuration.Scene.Depth.Expected.EvidenceSHAs,"depth")
 if string(owned.Scene.Policy)!=`{"algorithm":"farneback_depth_plane_v1"}` || owned.Scene.Scene.Expected.RequiredChecks[0]!="scene" {t.Fatal("scene configuration aliases mutable deployment memory")}
 if _,ok:=owned.Scene.Depth.Expected.EvidenceSHAs["depth"];!ok {t.Fatal("depth expected policy aliases deployment memory")}
 if ownTransferPreparation(TransferPreparation{}).Scene!=nil {t.Fatal("missing historical scene configuration was fabricated")}
}
