package interchange

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

func TestExportOverrideScopeRequiresRecipeAndPhysicalSource(t *testing.T) {
 for _, mode := range []string{"valid", "foreign_override", "input_sha", "edge", "source_hash", "source_identity", "canvas", "parent", "mask-valid", "mask-canvas", "cancelled"} { t.Run(mode, func(t *testing.T) {
  _, _, sourceRaw := importWorkingValidatorFixture(t)
  image, err := review.ParseWorkingImage(sourceRaw); if err != nil { t.Fatal(err) }
  canvas, err := canonical.Hash(image.Canvas); if err != nil { t.Fatal(err) }
  sourceEntry := v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(), SHA256:rawSHA(sourceRaw)}, Contract:"WorkingImage"}
  binding := v2input.SessionVersionBinding{AssetVersionID:uuid.New(), Source:sourceEntry.Ref, CanvasHash:canvas}
  inputSHA := image.Canvas.SourceSHA256
  if mode == "input_sha" { inputSHA = rawSHA([]byte("another original")) }
  overrideValue:=map[string]any{"schema_version":1,"input_sha256":inputSHA,"modules":map[string]any{"strength":0.7},"revision":1}
  if mode=="mask-valid"||mode=="mask-canvas"{
   mask:=review.MaskRef{Artifact:image.Pixels,Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"manual",Confidence:1}
   if mode=="mask-canvas"{mask.Canvas.Width++}
   overrideValue["mask_refs"]=[]any{map[string]any{"binding_id":"repair-1","purpose":"inpaint_target","mask":mask,"confirmed":true,"strength":1}}
  }
  overrideRaw,err:=canonical.Marshal(overrideValue);if err!=nil{t.Fatal(err)}
  entry := v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(), SHA256:rawSHA(overrideRaw)}, Contract:"Override"}
  style := v2input.Ref{ArtifactID:uuid.New(), SHA256:rawSHA([]byte("style"))}
  authored, err := canonical.Marshal(map[string]any{"schema_version":3, "source":binding, "base_style":style, "session_corrections":nil, "learning_version_id":nil, "overrides":[]v2input.Ref{entry.Ref}, "layer_stack":nil, "renderer_version":"v2.2", "parameter_delta":v2draft.CorrectionVector{}}); if err != nil { t.Fatal(err) }
  raw, err := v2draft.BuildFrozenRenderRecipe(authored, v2draft.ResolvedRecipeInputs{Binding:binding, BaseStyle:style, Style:v2draft.StyleBaseline{GainsRGB:[3]float64{1,1,1}, Saturation:1}, Overrides:[]v2draft.ResolvedCorrection{{Ref:entry.Ref}}, ICCLockSHA:rawSHA([]byte("icc")), ModelLockSHA:rawSHA([]byte("model"))}, nil); if err != nil { t.Fatal(err) }
  parent := v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(), SHA256:rawSHA(raw)}, Contract:"FrozenRenderRecipe"}
  parents := &exportMaskNodeStub{node:ExportClosureNode{Entry:parent, Body:raw, Children:[]v2input.Entry{entry}}}
  sources := &exportMaskNodeStub{node:ExportClosureNode{Entry:sourceEntry, Body:sourceRaw}}
  policy := &importValidatorStub{}
  scope := &ExportFrozenOverrideScope{parents:parents, sources:sources, recipes:[]exportRecipeBinding{{ref:parent.Ref, binding:binding}}, policy:exportReaderValidator{policy}}
  ctx := context.Background()
  switch mode {
  case "foreign_override": entry.Ref.ArtifactID = uuid.New()
  case "edge": parents.node.Children = nil
  case "source_hash": sources.node.Body = []byte("{}")
  case "source_identity": sources.node.Entry.Ref.ArtifactID = uuid.New()
  case "canvas": scope.recipes[0].binding.CanvasHash = rawSHA([]byte("foreign canvas"))
  case "parent": parents.node.Entry.Ref.ArtifactID = uuid.New()
  case "cancelled": var cancel context.CancelFunc; ctx, cancel = context.WithCancel(ctx); cancel()
  }
  _, err = scope.ExportDocumentChildren(ctx, entry, overrideRaw)
  if (err == nil) != (mode == "valid" || mode == "mask-valid") { t.Fatalf("unexpected override scope: %v", err) }
  expectedCalls := 0; if mode == "valid" || mode == "mask-valid" { expectedCalls = 1 }
  if policy.calls != expectedCalls { t.Fatalf("policy calls %d, want %d", policy.calls, expectedCalls) }
 }) }
}
