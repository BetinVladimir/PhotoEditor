package semanticproposal

import (
 "encoding/json"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// Historical replay only: the trusted compiler has already emitted batch.
// Rebuild the entire canonical config and hash; never alter persisted snapshots.
func omitNativeTransferQueue(plan *WorkflowPlan) error {
 if plan == nil { return v2input.ErrInvalid }
 var root map[string]json.RawMessage
 if json.Unmarshal(plan.config, &root) != nil || string(root["queue_class"]) != `"batch"` { return v2input.ErrInvalid }
 before, err := canonical.Hash(json.RawMessage(plan.config))
 if err != nil || before != plan.configSHA { return v2input.ErrConflict }
 delete(root, "queue_class")
 raw, err := canonical.Marshal(root)
 if err != nil { return err }
 sha, err := canonical.Hash(json.RawMessage(raw))
 if err != nil { return err }
 plan.config, plan.configSHA = raw, sha
 return nil
}
