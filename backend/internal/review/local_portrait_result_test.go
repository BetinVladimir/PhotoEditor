package review

import (
 "encoding/json"
 "testing"
 "github.com/google/uuid"
)

func TestLocalPortraitAutoApplyResponseUsesAdoptedRunIdentity(t *testing.T) {
 workflowID,runID,resultID:=uuid.New(),uuid.New(),uuid.New()
 selected:=LocalPortraitResult{RunID:runID,ResultID:resultID,Operation:"portrait.hair",Candidate:ArtifactRef{ID:uuid.New(),SHA256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
 response:=localPortraitAutoAppliedVersion(selected,uuid.New(),uuid.New(),uuid.New())
 raw,err:=json.Marshal(response);if err!=nil{t.Fatal(err)}
 var body map[string]any;if err=json.Unmarshal(raw,&body);err!=nil{t.Fatal(err)}
 if body["run_id"]!=runID.String()||body["run_id"]==workflowID.String(){t.Fatalf("response must identify the adopted run: %s",raw)}
 if body["result_id"]!=resultID.String(){t.Fatalf("response lost immutable result identity: %s",raw)}
}
