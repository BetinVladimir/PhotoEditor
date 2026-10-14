package interchange

import (
 "bytes"
 "context"
 "encoding/json"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// Separate nonrecursive readers prevent a dependency from authorizing itself.
type ExportFrozenOverrideScope struct {
 warps ExportDenseWarpOccurrenceAuthority
 parents, sources ExportClosureNodeResolver
 recipes []exportRecipeBinding
 policy ExportTypedDocumentValidator
}

func (s *ExportFrozenOverrideScope) ExportDocumentChildren(ctx context.Context, entry v2input.Entry, raw []byte) ([]v2input.Entry, error) {
 if err := ctx.Err(); err != nil { return nil, err }
 if s == nil || s.parents == nil || s.sources == nil || s.policy == nil { return nil, v2input.ErrInvalid }
 if entry.Contract == "Override" {
  if !validImportRef(entry.Ref) || len(raw) < 2 || len(raw) > 8<<20 || rawSHA(raw) != entry.Ref.SHA256 { return nil, v2input.ErrInvalid }
  normalized, err := canonical.Bytes(raw); if err != nil || !bytes.Equal(raw, normalized) { return nil, v2input.ErrInvalid }
  matched := false
  for _, expected := range s.recipes {
   if err := ctx.Err(); err != nil { return nil, err }
   parent := v2input.Entry{Ref:expected.ref, Contract:"FrozenRenderRecipe"}
   node, err := s.parents.ResolveExportNode(ctx, parent); if err != nil { return nil, err }
   if node.Entry != parent || rawSHA(node.Body) != parent.Ref.SHA256 { return nil, v2input.ErrInvalid }
   frozen, err := v2draft.DecodeFrozenRenderRecipe(node.Body); if err != nil { return nil, err }
   if frozen.Scalars.Source != expected.binding { return nil, v2input.ErrInvalid }
   var authored struct { Overrides []v2input.Ref `json:"overrides"` }
   if err := json.Unmarshal(frozen.Recipe, &authored); err != nil { return nil, err }
   refers := false
   for _, ref := range authored.Overrides { if ref.ArtifactID == entry.Ref.ArtifactID {
    if ref != entry.Ref { return nil, v2input.ErrInvalid }; refers = true
   } }
   if !refers { continue }
   edge := false
   for _, child := range node.Children { if child.Ref.ArtifactID == entry.Ref.ArtifactID {
    if child != entry { return nil, v2input.ErrInvalid }; edge = true
   } }
   if !edge { return nil, v2input.ErrInvalid }
   sourceEntry := v2input.Entry{Ref:expected.binding.Source, Contract:"WorkingImage"}
   source, err := s.sources.ResolveExportNode(ctx, sourceEntry); if err != nil { return nil, err }
   if source.Entry != sourceEntry || rawSHA(source.Body) != sourceEntry.Ref.SHA256 { return nil, v2input.ErrInvalid }
   image, err := review.ParseWorkingImage(source.Body); if err != nil { return nil, err }
   canvas, err := canonical.Hash(image.Canvas); if err != nil || canvas != expected.binding.CanvasHash { return nil, v2input.ErrInvalid }
   if err := validateExportResolvedOverride(raw, image); err != nil { return nil, err }
   if err := qualifyExportResolvedSourceWarps(ctx,source,s.warps); err != nil { return nil, err }
   matched = true
  }
  if !matched { return nil, v2input.ErrInvalid }
 }
 children, err := s.policy.ExportDocumentChildren(ctx, entry, append([]byte(nil), raw...)); if err != nil { return nil, err }
 if err := ctx.Err(); err != nil { return nil, err }
 return append([]v2input.Entry(nil), children...), nil
}

var _ ExportTypedDocumentValidator = (*ExportFrozenOverrideScope)(nil)
