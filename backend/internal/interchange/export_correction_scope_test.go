package interchange

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

func TestExportCorrectionScopeRequiresReferencingRecipeBinding(t *testing.T) {
 for _, mode := range []string{"valid", "foreign_plan", "missing_member", "parent_binding", "parent_hash", "missing_edge", "authority", "cancelled"} { t.Run(mode, func(t *testing.T) {
  ref := func(label string) v2input.Ref { return v2input.Ref{ArtifactID:uuid.New(), SHA256:rawSHA([]byte(label))} }
  binding := v2input.SessionVersionBinding{AssetVersionID:uuid.New(), Source:ref("source"), CanvasHash:rawSHA([]byte("canvas"))}
  planBinding := binding
  if mode == "missing_member" { planBinding.AssetVersionID = uuid.New() }
  planRaw, err := canonical.Marshal(map[string]any{"schema_version":1, "snapshot_digest":rawSHA([]byte("session")), "items":[]any{map[string]any{"binding":planBinding, "ordinal":0, "delta":v2draft.CorrectionVector{}, "disposition":"no_change", "reason":nil}}}); if err != nil { t.Fatal(err) }
  plan := v2input.Ref{ArtifactID:uuid.New(), SHA256:rawSHA(planRaw)}
  style := ref("style")
  authored, err := canonical.Marshal(map[string]any{"schema_version":3, "source":binding, "base_style":style, "session_corrections":plan, "learning_version_id":nil, "overrides":[]v2input.Ref{}, "layer_stack":nil, "renderer_version":"v2.2", "parameter_delta":v2draft.CorrectionVector{}}); if err != nil { t.Fatal(err) }
  raw, err := v2draft.BuildFrozenRenderRecipe(authored, v2draft.ResolvedRecipeInputs{Binding:binding, BaseStyle:style, Style:v2draft.StyleBaseline{GainsRGB:[3]float64{1,1,1}, Saturation:1}, Session:&v2draft.ResolvedCorrection{Ref:plan}, ICCLockSHA:rawSHA([]byte("icc")), ModelLockSHA:rawSHA([]byte("model"))}, nil); if err != nil { t.Fatal(err) }
  parent := v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(), SHA256:rawSHA(raw)}, Contract:"FrozenRenderRecipe"}
  entry := v2input.Entry{Ref:plan, Contract:"CorrectionPlan"}
  reader := &exportMaskNodeStub{node:ExportClosureNode{Entry:parent, Body:raw, Children:[]v2input.Entry{entry}}}
  policy := &exportSemanticsStub{deny:mode == "authority"}
  scope := &ExportFrozenSourceScope{reader:reader, recipes:[]exportRecipeBinding{{ref:parent.Ref, binding:binding}}, policy:policy}
  ctx := context.Background()
  switch mode {
  case "foreign_plan": entry.Ref.ArtifactID = uuid.New()
  case "parent_binding": scope.recipes[0].binding.AssetVersionID = uuid.New()
  case "parent_hash": reader.node.Body = []byte("{}")
  case "missing_edge": reader.node.Children = nil
  case "cancelled": var cancel context.CancelFunc; ctx, cancel = context.WithCancel(ctx); cancel()
  }
  err = scope.QualifyExportDependency(ctx, entry, planRaw, nil)
  if (err == nil) != (mode == "valid") { t.Fatalf("unexpected correction scope: %v", err) }
  expectedCalls := 0; if mode == "valid" || mode == "authority" { expectedCalls = 1 }
  if policy.calls != expectedCalls { t.Fatalf("policy calls %d, want %d", policy.calls, expectedCalls) }
 }) }
}

func TestExportFrozenSourceScopeLayerBinding(t *testing.T) {
 for _, mode := range []string{"valid", "foreign", "source", "child", "authority", "parent"} { t.Run(mode, func(t *testing.T) {
  ref := func(label string) v2input.Ref { return v2input.Ref{ArtifactID:uuid.New(), SHA256:rawSHA([]byte(label))} }
  binding := v2input.SessionVersionBinding{AssetVersionID:uuid.New(), Source:ref("source"), CanvasHash:rawSHA([]byte("canvas"))}
  layerRaw, err := canonical.Marshal(map[string]any{"schema_version":1, "source":binding, "layers":[]any{}}); if err != nil { t.Fatal(err) }
  layer := v2input.Ref{ArtifactID:uuid.New(), SHA256:rawSHA(layerRaw)}
  style := ref("style")
  authored, err := canonical.Marshal(map[string]any{"schema_version":3, "source":binding, "base_style":style, "session_corrections":nil, "learning_version_id":nil, "overrides":[]v2input.Ref{}, "layer_stack":layer, "renderer_version":"v2.2", "parameter_delta":v2draft.CorrectionVector{}}); if err != nil { t.Fatal(err) }
  raw, err := v2draft.BuildFrozenRenderRecipe(authored, v2draft.ResolvedRecipeInputs{Binding:binding, BaseStyle:style, Style:v2draft.StyleBaseline{GainsRGB:[3]float64{1,1,1}, Saturation:1}, ICCLockSHA:rawSHA([]byte("icc")), ModelLockSHA:rawSHA([]byte("model"))}, layerRaw); if err != nil { t.Fatal(err) }
  parent := v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(), SHA256:rawSHA(raw)}, Contract:"FrozenRenderRecipe"}
  entry := v2input.Entry{Ref:layer, Contract:"LayerStack"}
  reader := &exportMaskNodeStub{node:ExportClosureNode{Entry:parent, Body:raw, Children:[]v2input.Entry{entry}}}
  policy := &exportSemanticsStub{deny:mode == "authority"}
  scope := &ExportFrozenSourceScope{reader:reader, recipes:[]exportRecipeBinding{{ref:parent.Ref, binding:binding}}, policy:policy}
  switch mode {
  case "foreign": entry.Ref.ArtifactID = uuid.New()
  case "source":
   other := binding; other.Source.ArtifactID = uuid.New()
   layerRaw, err = canonical.Marshal(map[string]any{"schema_version":1, "source":other, "layers":[]any{}}); if err != nil { t.Fatal(err) }
   entry.Ref.SHA256 = rawSHA(layerRaw)
  case "child": reader.node.Children = nil
  case "parent": reader.node.Entry.Ref.ArtifactID = uuid.New()
  }
  err = scope.QualifyExportDependency(context.Background(), entry, layerRaw, nil)
  if (err == nil) != (mode == "valid") { t.Fatalf("unexpected layer scope: %v", err) }
  expectedCalls := 0; if mode == "valid" || mode == "authority" { expectedCalls = 1 }
  if policy.calls != expectedCalls { t.Fatalf("policy calls %d, want %d", policy.calls, expectedCalls) }
 }) }
}
