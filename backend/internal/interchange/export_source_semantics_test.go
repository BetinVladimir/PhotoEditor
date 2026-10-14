package interchange

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func TestExportSourceSemanticsFrozenLayerBinding(t *testing.T) {
 for _, mode := range []string{"valid", "version", "source", "canvas", "unknown", "hash", "cancelled", "authority"} { t.Run(mode, func(t *testing.T) {
  _, _, intent, frozen, _ := exportExecutionFixture(t)
  semantics, err := NewExportSourceSemantics(context.Background(), intent, frozen, &exportSemanticsStub{deny:mode == "authority"})
  if err != nil { t.Fatal(err) }
  var binding v2input.SessionVersionBinding
  for _, value := range semantics.bindings { binding = value; break }
  if binding.AssetVersionID == uuid.Nil { t.Fatal("fixture has no frozen source") }
  switch mode {
  case "version": binding.AssetVersionID = uuid.New()
  case "source": binding.Source.ArtifactID = uuid.New()
  case "canvas": binding.CanvasHash = rawSHA([]byte("foreign canvas"))
  }
  document := map[string]any{"schema_version":1, "source":binding, "layers":[]any{}}
  if mode == "unknown" { document["extra"] = true }
  raw, err := canonical.Marshal(document); if err != nil { t.Fatal(err) }
  entry := v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(), SHA256:rawSHA(raw)}, Contract:"LayerStack"}
  if mode == "hash" { raw = append(raw, ' ') }
  ctx := context.Background()
  if mode == "cancelled" { var cancel context.CancelFunc; ctx, cancel = context.WithCancel(ctx); cancel() }
  err = semantics.QualifyExportDependency(ctx, entry, raw, nil)
  if (err == nil) != (mode == "valid") { t.Fatalf("unexpected qualification: %v", err) }
 }) }
}

func TestExportSourceSemanticsRequiresFallback(t *testing.T) {
 _, _, intent, frozen, _ := exportExecutionFixture(t)
 if _, err := NewExportSourceSemantics(context.Background(), intent, frozen, nil); err == nil { t.Fatal("missing semantics accepted") }
 fallback := &exportSemanticsStub{}
 semantics, err := NewExportSourceSemantics(context.Background(), intent, frozen, fallback); if err != nil { t.Fatal(err) }
 if err := semantics.QualifyExportDependency(context.Background(), v2input.Entry{Contract:"Params"}, []byte("{}"), nil); err != nil || fallback.calls != 1 { t.Fatal("fallback not called", err) }
}

func TestExportSourceSemanticsCorrectionMembershipAndIndependentAuthority(t *testing.T) {
 for _, mode := range []string{"valid", "superset", "foreign", "source", "canvas", "duplicate", "delta", "authority", "hash", "unknown"} { t.Run(mode, func(t *testing.T) {
  _, _, intent, frozen, _ := exportExecutionFixture(t)
  fallback := &exportSemanticsStub{deny:mode == "authority"}
  semantics, err := NewExportSourceSemantics(context.Background(), intent, frozen, fallback); if err != nil { t.Fatal(err) }
  var binding v2input.SessionVersionBinding
  for _, selected := range semantics.bindings { binding = selected; break }
  switch mode {
  case "foreign": binding.AssetVersionID = uuid.New()
  case "source": binding.Source.ArtifactID = uuid.New()
  case "canvas": binding.CanvasHash = rawSHA([]byte("wrong canvas"))
  }
  item := func(source v2input.SessionVersionBinding, ordinal int) map[string]any {
   return map[string]any{"binding":source, "ordinal":ordinal, "delta":map[string]any{"ev":0, "log_r":0, "log_g":0, "log_b":0, "saturation_delta":0, "retouch_delta":0}, "disposition":"no_change", "reason":nil}
  }
  first := item(binding, 0)
  items := []any{first}
  if mode == "superset" {
   other := binding; other.AssetVersionID = uuid.New(); other.Source.ArtifactID = uuid.New()
   items = []any{item(other, 2), item(binding, 7)}
  }
  if mode == "duplicate" { items = append(items, item(binding, 1)) }
  if mode == "delta" { first["delta"].(map[string]any)["ev"] = 2 }
  document := map[string]any{"schema_version":1, "snapshot_digest":rawSHA([]byte("original session snapshot")), "items":items}
  if mode == "unknown" { document["extra"] = true }
  raw, err := canonical.Marshal(document); if err != nil { t.Fatal(err) }
  entry := v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(), SHA256:rawSHA(raw)}, Contract:"CorrectionPlan"}
  if mode == "hash" { raw = append(raw, ' ') }
  err = semantics.QualifyExportDependency(context.Background(), entry, raw, nil)
  expected := mode == "valid" || mode == "superset"
  if (err == nil) != expected { t.Fatalf("unexpected qualification: %v", err) }
  expectedCalls := 0
  if expected || mode == "authority" { expectedCalls = 1 }
  if fallback.calls != expectedCalls { t.Fatalf("authority calls %d, want %d", fallback.calls, expectedCalls) }
 }) }
}
