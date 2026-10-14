package provenance

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
)

// VerifyDelegatedRenderInputBytes applies the same exact-version binary and
// background byte checks as session-backed signing, using a catalog already
// scoped to the delegated ancestor task. It does not grant that catalog scope.
func VerifyDelegatedRenderInputBytes(ctx context.Context, catalog v2draft.ArtifactCatalog,
	objects SigningObjects, tenant uuid.UUID, inventory v2draft.TaskInputEvidence,
	workICC string) error {
	if err := ctx.Err(); err != nil { return err }
	if catalog == nil || objects == nil || tenant == uuid.Nil || inventory.Entries == nil ||
		inventory.Occurrences == nil || !digest(workICC) { return ErrInvalid }
	evidence := SigningRenderClosureEvidence{inventory: inventory}
	ready := ReadySigningExport{Tenant: tenant}
	if err := verifySigningRenderBinaryBytes(ctx, catalog, objects, auth.Identity{}, ready, evidence); err != nil {
		return err
	}
	if err := verifyDelegatedRenderWorkingArrays(ctx, catalog, objects, tenant, inventory); err != nil {
		return err
	}
	if err := verifyDelegatedRenderMaskRefArrays(ctx, catalog, objects, tenant, inventory); err != nil {
		return err
	}
	return verifySigningRenderBackgroundBytes(ctx, catalog, objects, auth.Identity{}, ready, evidence, workICC)
}
