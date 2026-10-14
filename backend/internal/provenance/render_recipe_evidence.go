package provenance

import (
	"bytes"
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
)

// ReadSigningRenderRecipe validates frozen render Params through the same
// strict decoder used by render admission. It does not grant a SigningAction:
// adopted ImageResult/report, masks, model locks and human review must still be
// independently proved before any generation/review claims can be emitted.
func ReadSigningRenderRecipe(ctx context.Context, documents SigningTaskDocumentReader, who auth.Identity, ready ReadySigningExport, task CanonicalSigningTask) (v2draft.FrozenRenderRecipe, error) {
	var zero v2draft.FrozenRenderRecipe
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if documents == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || ready.Tenant == uuid.Nil || ready.Project == uuid.Nil || ready.ExportID == uuid.Nil || task.Operation != "render.compose" || task.Version != "1" || normalizeCanonicalSigningTask(&task) != nil {
		return zero, ErrInvalid
	}
	raw, err := documents.ReadSigningTaskDocument(ctx, who, ready, task, task.Params, 1<<20)
	if err != nil {
		return zero, err
	}
	if !pinned(raw, task.Params, 1<<20) {
		return zero, ErrInvalid
	}
	owned := append([]byte(nil), raw...)
	recipe, err := v2draft.DecodeFrozenRenderRecipe(owned)
	if err != nil {
		return zero, ErrInvalid
	}
	current, err := documents.ReadSigningTaskDocument(ctx, who, ready, task, task.Params, 1<<20)
	if err != nil {
		return zero, err
	}
	if !bytes.Equal(current, owned) {
		return zero, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return recipe, nil
}
