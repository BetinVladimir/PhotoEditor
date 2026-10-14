package semanticproposal

import (
 "encoding/json"
 "testing"
 "photoeditor/backend/internal/canonical"
)

func TestHistoricalNativeTransferQueuePreservesFullConfig(t *testing.T) {
 raw := json.RawMessage(`{"queue_class":"batch","project_id":"project","v27_semantic":{"nodes":[{"task_id":"task","input_digest":"pin"}],"qualification":{"report":"original"}}}`)
 sha, err := canonical.Hash(raw)
 if err != nil { t.Fatal(err) }
 p := WorkflowPlan{config: raw, configSHA: sha}
 if err := omitNativeTransferQueue(&p); err != nil { t.Fatal(err) }
 expected := json.RawMessage(`{"project_id":"project","v27_semantic":{"nodes":[{"task_id":"task","input_digest":"pin"}],"qualification":{"report":"original"}}}`)
 expectedBytes, _ := canonical.Bytes(expected)
 expectedSHA, _ := canonical.Hash(expected)
 if string(p.config) != string(expectedBytes) || p.configSHA != expectedSHA { t.Fatal("historical config changed beyond queue") }
 if string(raw) != `{"queue_class":"batch","project_id":"project","v27_semantic":{"nodes":[{"task_id":"task","input_digest":"pin"}],"qualification":{"report":"original"}}}` { t.Fatal("source buffer mutated") }
 if err := omitNativeTransferQueue(&p); err == nil { t.Fatal("non-batch compiler input accepted") }
 broken := WorkflowPlan{config: raw, configSHA: "foreign"}
 if err := omitNativeTransferQueue(&broken); err == nil || string(broken.config) != string(raw) || broken.configSHA != "foreign" { t.Fatal("bad hash accepted or changed") }
}
