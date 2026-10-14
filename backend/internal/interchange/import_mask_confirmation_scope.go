package interchange

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// ImportMaskConfirmationScope identifies the exact archived Override inside
// one import entry. A mask artifact alone is not proof that it belongs to the
// recipe the user reviewed.
type ImportMaskConfirmationScope struct {
	Workflow uuid.UUID
	Entry    uuid.UUID
	Recipe   v2input.Ref
	Override v2input.Ref
}

type importConfirmationScopeKey struct{}
type importRecipeConfirmationKey struct{}
type importPreparedOverrideMaskKey struct{}
type importConfirmedMaskKey struct{}
type importConfirmedMaskScope struct {
	parent   ImportMaskConfirmationScope
	prepared v2input.Ref
}
type importPreparedOverrideMaskScope struct{ parent, mask v2input.Ref }
type importRecipeConfirmationScope struct {
	workflow  uuid.UUID
	entry     uuid.UUID
	recipe    v2input.Ref
	overrides map[v2input.Ref]bool
}

func withImportRecipeConfirmationScope(ctx context.Context, workflow, entry uuid.UUID, recipe v2input.Ref, overrides []v2input.Ref) context.Context {
	owned := make(map[v2input.Ref]bool, len(overrides))
	for _, ref := range overrides {
		owned[ref] = true
	}
	return context.WithValue(ctx, importRecipeConfirmationKey{}, importRecipeConfirmationScope{workflow: workflow, entry: entry, recipe: recipe, overrides: owned})
}

func withImportConfirmationScope(ctx context.Context, workflow, entry uuid.UUID, ref v2input.Ref, contract string) context.Context {
	if contract != "Override" {
		return ctx
	}
	recipe, ok := ctx.Value(importRecipeConfirmationKey{}).(importRecipeConfirmationScope)
	if !ok || recipe.workflow != workflow || recipe.entry != entry || !validImportRef(recipe.recipe) || !recipe.overrides[ref] {
		return ctx
	}
	return context.WithValue(ctx, importConfirmationScopeKey{}, ImportMaskConfirmationScope{Workflow: workflow, Entry: entry, Recipe: recipe.recipe, Override: ref})
}

func confirmedOverrideScope(ctx context.Context, raw []byte) (ImportMaskConfirmationScope, error) {
	scope, ok := ctx.Value(importConfirmationScopeKey{}).(ImportMaskConfirmationScope)
	if !ok || scope.Workflow == uuid.Nil || scope.Entry == uuid.Nil || !validImportRef(scope.Recipe) || !validImportRef(scope.Override) || rawSHA(raw) != scope.Override.SHA256 {
		return ImportMaskConfirmationScope{}, v2input.ErrInvalid
	}
	return scope, nil
}

func withPreparedOverrideMaskScope(ctx context.Context, parent v2input.Ref, mask v2input.Ref) context.Context {
	if !validImportRef(parent) || !validImportRef(mask) {
		return ctx
	}
	return context.WithValue(ctx, importPreparedOverrideMaskKey{}, importPreparedOverrideMaskScope{parent: parent, mask: mask})
}

func withImportConfirmedMaskScope(ctx context.Context, scope ImportMaskConfirmationScope, archived review.MaskRef) context.Context {
	parent, ok := ctx.Value(importConfirmationScopeKey{}).(ImportMaskConfirmationScope)
	if !ok || parent != scope || scope.Workflow == uuid.Nil || scope.Entry == uuid.Nil || !validImportRef(scope.Recipe) || !validImportRef(scope.Override) || archived.Selector != "person" || archived.ObjectID != nil || !validImportRef(v2input.Ref{ArtifactID: archived.Artifact.ID, SHA256: archived.Artifact.SHA256}) {
		return ctx
	}
	prepared := v2input.Ref{ArtifactID: uuid.NewSHA1(scope.Workflow, []byte("interchange:dependency:"+scope.Entry.String()+":MaskPixels:"+archived.Artifact.ID.String())), SHA256: archived.Artifact.SHA256}
	return context.WithValue(ctx, importConfirmedMaskKey{}, importConfirmedMaskScope{parent: scope, prepared: prepared})
}

func hasImportedPersonMaskContext(ctx context.Context, mask review.MaskRef) bool {
	if exact, ok := ctx.Value(importConfirmedMaskKey{}).(importConfirmedMaskScope); ok && exact.prepared.ArtifactID == mask.Artifact.ID && exact.prepared.SHA256 == mask.Artifact.SHA256 &&
		exact.parent.Workflow != uuid.Nil && exact.parent.Entry != uuid.Nil && validImportRef(exact.parent.Recipe) && validImportRef(exact.parent.Override) {
		return true
	}
	if scope, ok := ctx.Value(importPreparedOverrideMaskKey{}).(importPreparedOverrideMaskScope); ok && validImportRef(scope.parent) &&
		scope.mask.ArtifactID == mask.Artifact.ID && scope.mask.SHA256 == mask.Artifact.SHA256 {
		return true
	}
	return false
}
