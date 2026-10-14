package v2input

import (
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

func TestCaptureQueueClassFrozenBeforeConfigHash(t *testing.T){
 config:=CaptureDraftConfig{ProjectID:uuid.New(),QueueClass:"capture"}
 body,err:=canonical.Marshal(config);if err!=nil{t.Fatal(err)}
 var wire map[string]json.RawMessage;if err=json.Unmarshal(body,&wire);err!=nil{t.Fatal(err)}
 if string(wire["queue_class"])!=`"capture"`{t.Fatal("capture queue not frozen")}
 withQueue,err:=canonical.Hash(config);if err!=nil{t.Fatal(err)}
 config.QueueClass=""
 legacy,err:=canonical.Marshal(config);if err!=nil{t.Fatal(err)}
 // Decode into a fresh map: Unmarshal otherwise retains previously present keys.
 var legacyWire map[string]json.RawMessage;if err=json.Unmarshal(legacy,&legacyWire);err!=nil{t.Fatal(err)}
 if _,present:=legacyWire["queue_class"];present{t.Fatal("historical config acquired a field")}
 legacyHash,err:=canonical.Hash(config);if err!=nil{t.Fatal(err)}
 if legacyHash==withQueue{t.Fatal("queue class not bound to config digest")}
}
