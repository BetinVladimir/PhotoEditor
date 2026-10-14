package interchange

import (
 "context"
 "encoding/json"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// ExportFrozenSourceScope rechecks every selected recipe referring to a
// correction plan or layer stack. Provenance remains an independent policy check.
type ExportFrozenSourceScope struct {
 reader ExportClosureNodeResolver
 recipes []exportRecipeBinding
 policy ExportDependencySemantics
}

func NewExportFrozenSourceScope(ctx context.Context, intent CreateIntent, frozen FrozenExportDocuments, reader ExportClosureNodeResolver, policy ExportDependencySemantics) (*ExportFrozenSourceScope, error) {
 if err := ctx.Err(); err != nil { return nil, err }
 if reader == nil || policy == nil { return nil, v2input.ErrInvalid }
 checked, _, err := DecodeFrozenExportDocuments(ctx, intent, frozen.Body()); if err != nil { return nil, err }
 var payload exportFreezePayload
 if err := json.Unmarshal(checked.Body(), &payload); err != nil { return nil, err }
 var snapshot v2input.SessionInputSnapshot
 if err := decodeImportDocument([]byte(payload.SnapshotJCS), &snapshot); err != nil { return nil, err }
 result := &ExportFrozenSourceScope{reader:reader, policy:policy}
 for _, item := range snapshot.Items { result.recipes = append(result.recipes, exportRecipeBinding{ref:item.Recipe, binding:item.Binding}) }
 return result, ctx.Err()
}

func (s *ExportFrozenSourceScope) QualifyExportDependency(ctx context.Context, entry v2input.Entry, raw []byte, hints []v2draft.LayerRef) error {
 if err := ctx.Err(); err != nil { return err }
 if s == nil || s.reader == nil || s.policy == nil { return v2input.ErrInvalid }
 if entry.Contract == "CorrectionPlan" || entry.Contract == "LayerStack" {
  if !validImportRef(entry.Ref) || len(raw) < 2 || len(raw) > 8<<20 || rawSHA(raw) != entry.Ref.SHA256 { return v2input.ErrInvalid }
  matched := false
  for _, expected := range s.recipes {
   if err := ctx.Err(); err != nil { return err }
   parent := v2input.Entry{Ref:expected.ref, Contract:"FrozenRenderRecipe"}
   node, err := s.reader.ResolveExportNode(ctx, parent); if err != nil { return err }
   if node.Entry != parent || rawSHA(node.Body) != parent.Ref.SHA256 { return v2input.ErrInvalid }
   recipe, err := v2draft.DecodeFrozenRenderRecipe(node.Body); if err != nil { return err }
   if recipe.Scalars.Source != expected.binding { return v2input.ErrInvalid }
   var authored struct { Corrections *v2input.Ref `json:"session_corrections"`; Layers *v2input.Ref `json:"layer_stack"` }
   if err := json.Unmarshal(recipe.Recipe, &authored); err != nil { return err }
   reference := authored.Corrections
   if entry.Contract == "LayerStack" { reference = authored.Layers }
   if reference == nil || reference.ArtifactID != entry.Ref.ArtifactID { continue }
   if *reference != entry.Ref { return v2input.ErrInvalid }
   found := false
   for _, child := range node.Children { if child.Ref.ArtifactID == entry.Ref.ArtifactID {
    if child != entry { return v2input.ErrInvalid }; found = true
   } }
   if !found { return v2input.ErrInvalid }
   if entry.Contract == "CorrectionPlan" {
    if err := v2draft.ValidateCorrectionPlan(raw, expected.binding); err != nil { return err }
   } else {
    if _, err := v2draft.ValidateLayerStack(raw, expected.binding); err != nil { return err }
   }
   matched = true
  }
  if !matched { return v2input.ErrInvalid }
 }
 if err := s.policy.QualifyExportDependency(ctx, entry, append([]byte(nil), raw...), hints); err != nil { return err }
 return ctx.Err()
}

var _ ExportDependencySemantics = (*ExportFrozenSourceScope)(nil)
