package v2render

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// originalBinding preserves the ordinary full-recipe source anchor. The
// current native source is an occurrence and may not relabel historical masks.
func (s SQLAuthoredNativeReplayCatalog) originalBinding(ctx context.Context, who auth.Identity, tenant uuid.UUID) (v2input.SessionVersionBinding, error) {
	var empty v2input.SessionVersionBinding
	r := s.request
	before, found, err := ReadAuthoredInstructionSource(ctx, s.Scheduler, tenant, r.Consumer.Parent, r.Consumer.Ordinal)
	if err != nil {
		return empty, err
	}
	if !found || before.Actor != who.UserID || before.Session != who.SessionID || before.ProgramSHA != r.Consumer.ProgramSHA || before.Source != r.Consumer.Source {
		return empty, v2input.ErrInvalid
	}
	raw, err := s.read(ctx, who, tenant, before.OriginalRecipe)
	if err != nil {
		return empty, err
	}
	if !bytes.Equal(raw, []byte(before.RecipeJCS)) {
		return empty, v2input.ErrInvalid
	}
	recipe, err := v2draft.DecodeFrozenRenderRecipe(raw)
	if err != nil {
		return empty, err
	}
	anchor := recipe.Scalars.Source
	if !authoredBindingValid(anchor) {
		return empty, v2input.ErrInvalid
	}
	// Require the actual anchor's protected physical location before traversal.
	if _, err = s.Lookup(ctx, who, tenant, v2draft.LayerRef{Ref: anchor.Source, Contract: "WorkingImage"}); err != nil {
		return empty, err
	}
	after, present, err := ReadAuthoredInstructionSource(ctx, s.Scheduler, tenant, r.Consumer.Parent, r.Consumer.Ordinal)
	if err != nil {
		return empty, err
	}
	a, e := canonical.Marshal(before)
	b, f := canonical.Marshal(after)
	if !present || e != nil || f != nil || !bytes.Equal(a, b) {
		return empty, v2input.ErrConflict
	}
	return anchor, nil
}
