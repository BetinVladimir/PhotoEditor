package workflow

import (
 "encoding/json"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/review"
)

func TestApplyCullingPlanSourceMeasurementAndSelectionBarrier(t *testing.T){
 for _,mode:=range []string{"fixed","adaptive"}{
  plan,err:=CompileApplyV4Plan(json.RawMessage(`{"mode":"`+mode+`","extensions":{"culling":{"enabled":true}}}`),5000);if err!=nil{t.Fatal(err)}
  index:=map[string]int{};stats:=0;for i,s:=range plan{index[s.Phase]=i;if s.Phase=="stats"{stats++}}
  if stats!=1||index["stats"]>=index["resolve"]||index["resolve"]>=index["cull_embedding"]||index["cull_features"]>=index["cull_pack"]||index["cull_rank"]>=index["cull_review"]||index["cull_review"]>=index["color"]{t.Fatalf("unsafe culling order: %+v",plan)}
  if plan[index["cull_pack"]].Cardinality!="group"||plan[index["cull_rank"]].Cardinality!="aggregate"||plan[index["cull_review"]].Cardinality!="review"{t.Fatal("wrong culling cardinality")}
 }
 if (5000+applyCullPackSize-1)/applyCullPackSize>64{t.Fatal("pack fan-in exceeds declared inputs")}
}

func TestApplyCullingProjectionExactSnapshotBindings(t *testing.T){
 item:=applyInput{ItemID:uuid.New(),AssetID:uuid.New(),VersionID:uuid.New()}
 group:=review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("a",64)};proposal:=review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("b",64)}
 gr,_:=json.Marshal(map[string]any{"items":[]any{map[string]any{"item_id":item.ItemID,"scene_id":"scene1","exact_group_id":"exact1","burst_group_id":"burst1"}}})
 ranked:=map[string]any{"item_id":item.ItemID,"scene_id":"scene1","rank":1,"score":.8,"selected":true,"manual":false,"eye_review":true}
 makeProposal:=func()[]byte{raw,_:=json.Marshal(map[string]any{"groups":group,"never_delete":true,"items":[]any{ranked}});return raw}
 raw,err:=applyCullingProjection([]applyInput{item},gr,makeProposal(),group,proposal);if err!=nil{t.Fatal(err)}
 var value struct{Items []struct{Asset uuid.UUID `json:"asset_id"`;Version uuid.UUID `json:"asset_version_id"`;Eye string `json:"eye_state"`} `json:"items"`};if json.Unmarshal(raw,&value)!=nil||value.Items[0].Asset!=item.AssetID||value.Items[0].Version!=item.VersionID||value.Items[0].Eye!="unknown"{t.Fatal("source identity or honest eye state lost")}
 ranked["item_id"]=uuid.New();if _,err=applyCullingProjection([]applyInput{item},gr,makeProposal(),group,proposal);err==nil{t.Fatal("foreign item accepted")}
 ranked["item_id"]=item.ItemID;ranked["eye_review"]=false;if _,err=applyCullingProjection([]applyInput{item},gr,makeProposal(),group,proposal);err==nil{t.Fatal("unobserved eyes auto-confirmed")}
}

func TestApplyCullingDINOResourceFloor(t *testing.T){
 raw,err:=applyCullProfile("features.scene","mps",strings.Repeat("a",64),24000000,4096,512);if err!=nil{t.Fatal(err)}
 estimate,err:=ParseResourceEstimate(raw);if err!=nil{t.Fatal(err)}
 if estimate.RAMBytes<(1<<30)+24000000*96||estimate.ScratchBytes<24000000*12||estimate.VRAMBytes!=1<<30{t.Fatalf("DINO resources undercounted: %+v",estimate)}
}
