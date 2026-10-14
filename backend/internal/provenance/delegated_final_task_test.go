package provenance

import (
 "encoding/json"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

func TestDecodeDelegatedFinalTaskEvidence(t *testing.T) {
 ready := ReadySigningExport{Task: uuid.New(), Run: uuid.New(),
  Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}}
 params := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
 wire := map[string]any{"task": ready.Task, "run": ready.Run,
  "result": uuid.New(), "attempt": 1, "fence": int64(3),
  "operation": "export.encode", "version": "1",
  "input_digest": strings.Repeat("c", 64),
  "result_digest": strings.Repeat("d", 64), "params": params,
  "inputs": []any{}, "outputs": []v2input.Ref{ready.Ref},
  "metrics": map[string]any{}, "review_required": false}
 raw, err := json.Marshal(wire)
 if err != nil { t.Fatal(err) }
 if _, err := DecodeDelegatedFinalTaskEvidence(raw, ready); err != nil {
  t.Fatal("valid final export task rejected", err)
 }
 if _, err := DecodeDelegatedSameRunTaskEvidence(raw, ready, ready.Task); err != nil {
  t.Fatal("valid same-run task rejected", err)
 }
 if _, err := DecodeDelegatedAncestorTaskEvidence(raw, ready, ready.Task); err != nil {
  t.Fatal("valid ancestor task rejected", err)
 }
 if _, err := DecodeDelegatedSameRunTaskEvidence(raw, ready, uuid.New()); err == nil {
  t.Fatal("unrequested same-run task accepted")
 }
 if _, err := DecodeDelegatedAncestorTaskEvidence(raw, ready, uuid.New()); err == nil {
  t.Fatal("unrequested ancestor task accepted")
 }
 wire["run"] = uuid.New()
 changed, _ := json.Marshal(wire)
 if _, err := DecodeDelegatedSameRunTaskEvidence(changed, ready, ready.Task); err == nil {
  t.Fatal("foreign run accepted")
 }
 wire["run"] = ready.Run
 wire["task"] = uuid.New()
 changed, _ = json.Marshal(wire)
 if _, err := DecodeDelegatedFinalTaskEvidence(changed, ready); err == nil {
  t.Fatal("foreign task accepted")
 }
 wire["task"] = ready.Task
 wire["outputs"] = []v2input.Ref{{ArtifactID: uuid.New(), SHA256: ready.Ref.SHA256}}
 changed, _ = json.Marshal(wire)
 if _, err := DecodeDelegatedFinalTaskEvidence(changed, ready); err == nil {
  t.Fatal("substituted output accepted")
 }
}
