package interchange

import (
 "bytes"
 "context"
 "encoding/json"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// ExportSourceSemantics binds layer operations and correction membership to the immutable export selection.
// Other dependency contracts still require independently installed semantics.
type ExportSourceSemantics struct {
 bindings map[uuid.UUID]v2input.SessionVersionBinding
 fallback ExportDependencySemantics
}

func NewExportSourceSemantics(ctx context.Context, intent CreateIntent, frozen FrozenExportDocuments, fallback ExportDependencySemantics) (*ExportSourceSemantics, error) {
 if err := ctx.Err(); err != nil { return nil, err }
 if fallback == nil { return nil, v2input.ErrInvalid }
 checked, _, err := DecodeFrozenExportDocuments(ctx, intent, frozen.Body()); if err != nil { return nil, err }
 var payload exportFreezePayload
 if err := json.Unmarshal(checked.Body(), &payload); err != nil { return nil, err }
 var snapshot v2input.SessionInputSnapshot
 if err := decodeImportDocument([]byte(payload.SnapshotJCS), &snapshot); err != nil { return nil, err }
 result := &ExportSourceSemantics{bindings: map[uuid.UUID]v2input.SessionVersionBinding{}, fallback: fallback}
 for _, item := range snapshot.Items {
  if old, exists := result.bindings[item.Binding.AssetVersionID]; exists && old != item.Binding { return nil, v2input.ErrInvalid }
  result.bindings[item.Binding.AssetVersionID] = item.Binding
 }
 return result, ctx.Err()
}

func (s *ExportSourceSemantics) QualifyExportDependency(ctx context.Context, entry v2input.Entry, raw []byte, hints []v2draft.LayerRef) error {
 if err := ctx.Err(); err != nil { return err }
 if s == nil { return v2input.ErrInvalid }
 if entry.Contract != "LayerStack" && entry.Contract != "CorrectionPlan" {
  if s.fallback == nil { return v2input.ErrInvalid }
  return s.fallback.QualifyExportDependency(ctx, entry, append([]byte(nil), raw...), hints)
 }
 if !validImportRef(entry.Ref) || len(raw) < 2 || len(raw) > 8<<20 || rawSHA(raw) != entry.Ref.SHA256 { return v2input.ErrInvalid }
 normalized, err := canonical.Bytes(raw)
 if err != nil || !bytes.Equal(raw, normalized) { return v2input.ErrInvalid }
 if entry.Contract == "CorrectionPlan" {
  // A session plan may include unselected frames. Its snapshot provenance and
  // every additional source remain the independently installed policy's duty.
  var plan struct { Items []struct { Binding v2input.SessionVersionBinding `json:"binding"` } `json:"items"` }
  if json.Unmarshal(raw, &plan) != nil || len(plan.Items) < 1 || len(plan.Items) > 5000 { return v2input.ErrInvalid }
  matched := false
  for _, item := range plan.Items {
   if selected, exists := s.bindings[item.Binding.AssetVersionID]; exists {
    if selected != item.Binding { return v2input.ErrInvalid }
    matched = true
   }
  }
  if !matched { return v2input.ErrInvalid }
  if err := v2draft.ValidateCorrectionPlan(raw, plan.Items[0].Binding); err != nil { return err }
  if s.fallback == nil { return v2input.ErrInvalid }
  if err := s.fallback.QualifyExportDependency(ctx, entry, append([]byte(nil), raw...), hints); err != nil { return err }
  return ctx.Err()
 }
 var document struct { Source v2input.SessionVersionBinding `json:"source"` }
 if err := json.Unmarshal(raw, &document); err != nil { return v2input.ErrInvalid }
 binding, exists := s.bindings[document.Source.AssetVersionID]
 if !exists || binding != document.Source { return v2input.ErrInvalid }
 if _, err := v2draft.ValidateLayerStack(raw, binding); err != nil { return err }
 if s.fallback == nil { return v2input.ErrInvalid }
 if err := s.fallback.QualifyExportDependency(ctx, entry, append([]byte(nil), raw...), hints); err != nil { return err }
 return ctx.Err()
}

var _ ExportDependencySemantics = (*ExportSourceSemantics)(nil)
