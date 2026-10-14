package v2input

import (
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

func TestSessionAnalysisQueueClassBindsConfigHash(t *testing.T){
 config:=sessionAnalysisConfig{ProjectID:uuid.New(),SessionID:uuid.New(),QueueClass:"batch"}
 body,err:=canonical.Marshal(config);if err!=nil{t.Fatal(err)}
 var fields map[string]json.RawMessage;if err=json.Unmarshal(body,&fields);err!=nil{t.Fatal(err)}
 if string(fields["queue_class"])!=`"batch"`{t.Fatal("queue class not encoded")}
 frozenHash,err:=canonical.Hash(config);if err!=nil{t.Fatal(err)}
 config.QueueClass=""
 legacy,err:=canonical.Marshal(config);if err!=nil{t.Fatal(err)}
 var historical map[string]json.RawMessage;if err=json.Unmarshal(legacy,&historical);err!=nil{t.Fatal(err)}
 if _,exists:=historical["queue_class"];exists{t.Fatal("historical config rewritten")}
 legacyHash,err:=canonical.Hash(config);if err!=nil||legacyHash==frozenHash{t.Fatal("class not bound to hash",err)}
}
