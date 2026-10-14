package semanticproposal

import (
 "encoding/json"
 "photoeditor/backend/internal/v2input"
)

// Reads only the saved queue choice. Callers must still compare the entire
// recompiled config and hash against the authoritative saved plan.
func nativePreparationLegacyQueue(config string) (bool, error) {
 var root map[string]json.RawMessage
 if json.Unmarshal([]byte(config), &root) != nil || root == nil { return false, v2input.ErrInvalid }
 raw, present := root["queue_class"]
 if !present { return true, nil }
 var queue string
 if json.Unmarshal(raw, &queue) != nil || queue != "batch" { return false, v2input.ErrInvalid }
 return false, nil
}
