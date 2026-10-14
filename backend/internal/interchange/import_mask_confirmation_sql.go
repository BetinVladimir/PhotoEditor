package interchange

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// SQLImportConfirmedMaskAuthority reads the current, independently recorded
// user decision. The archive's confirmed flag never supplies authority.
type SQLImportConfirmedMaskAuthority struct {
	Store SQLPreparedImportArtifactStore
}

func (a SQLImportConfirmedMaskAuthority) ready() bool {
	return a.Store.DB != nil && a.Store.Tenant != uuid.Nil && a.Store.Identity.Workflow != uuid.Nil && len(a.Store.Parsed) >= 2
}

func (a SQLImportConfirmedMaskAuthority) live(ctx context.Context, scope ImportMaskConfirmationScope, archived review.MaskRef, old, target v2input.SessionVersionBinding) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if !a.ready() || scope.Workflow != a.Store.Identity.Workflow || scope.Entry == uuid.Nil || !validImportRef(scope.Recipe) || !validImportRef(scope.Override) ||
		old.AssetVersionID == uuid.Nil || target.AssetVersionID == uuid.Nil || old.AssetVersionID == target.AssetVersionID ||
		!validImportRef(target.Source) || archived.Selector != "person" || archived.ObjectID != nil || archived.ModelLockID == nil ||
		!validImportRef(v2input.Ref{ArtifactID: archived.Artifact.ID, SHA256: archived.Artifact.SHA256}) {
		return 0, v2input.ErrInvalid
	}
	var epoch *int64
	err := database.TenantTransaction(ctx, a.Store.DB, a.Store.Tenant, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		return tx.QueryRow(ctx, `SELECT photo.v30_import_person_mask_confirmation_live($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			a.Store.Tenant, scope.Workflow, scope.Entry, scope.Recipe.ArtifactID, scope.Recipe.SHA256,
			scope.Override.ArtifactID, scope.Override.SHA256, archived.Artifact.ID, archived.Artifact.SHA256,
			old.AssetVersionID, target.AssetVersionID, string(a.Store.Parsed)).Scan(&epoch)
	})
	if err != nil {
		return 0, err
	}
	if epoch == nil || *epoch < 1 {
		return 0, &ImportRecipeUnsupported{Reason: "imported_mask_requires_new_confirmation"}
	}
	return *epoch, ctx.Err()
}

func (a SQLImportConfirmedMaskAuthority) QualifyArchivedMaskConfirmation(ctx context.Context, scope ImportMaskConfirmationScope, archived review.MaskRef, old, target v2input.SessionVersionBinding) (int64, error) {
	return a.live(ctx, scope, archived, old, target)
}

func (a SQLImportConfirmedMaskAuthority) QualifyImportedMaskConfirmation(ctx context.Context, scope ImportMaskConfirmationScope, archived, prepared review.MaskRef, old, target v2input.SessionVersionBinding, epoch int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if epoch < 1 || prepared.Selector != "person" || prepared.ObjectID != nil || prepared.ModelLockID == nil ||
		archived.ModelLockID == nil || *prepared.ModelLockID != *archived.ModelLockID ||
		prepared.Artifact.SHA256 != archived.Artifact.SHA256 || prepared.Canvas != archived.Canvas {
		return v2input.ErrInvalid
	}
	canvas, err := canonical.Hash(prepared.Canvas)
	if err != nil || canvas != old.CanvasHash || canvas != target.CanvasHash {
		return v2input.ErrInvalid
	}
	expected := uuid.NewSHA1(scope.Workflow, []byte("interchange:dependency:"+scope.Entry.String()+":MaskPixels:"+archived.Artifact.ID.String()))
	if prepared.Artifact.ID != expected {
		return v2input.ErrInvalid
	}
	current, err := a.live(ctx, scope, archived, old, target)
	if err != nil {
		return err
	}
	if current != epoch {
		return &ImportRecipeUnsupported{Reason: "imported_mask_confirmation_changed"}
	}
	return ctx.Err()
}

func (a SQLImportConfirmedMaskAuthority) QualifyPreparedMaskConfirmation(ctx context.Context, prepared review.MaskRef, target v2input.SessionVersionBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !a.ready() || target.AssetVersionID == uuid.Nil || prepared.Selector != "person" || prepared.ObjectID != nil || prepared.ModelLockID == nil || !validImportRef(target.Source) {
		return v2input.ErrInvalid
	}
	canvas, err := canonical.Hash(prepared.Canvas)
	if err != nil || canvas != target.CanvasHash {
		return v2input.ErrInvalid
	}
	var epoch *int64
	err = database.TenantTransaction(ctx, a.Store.DB, a.Store.Tenant, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		return tx.QueryRow(ctx, `SELECT photo.v30_import_person_mask_prepared_live($1,$2,$3,$4,$5,$6)`,
			a.Store.Tenant, a.Store.Identity.Workflow, target.AssetVersionID,
			prepared.Artifact.ID, prepared.Artifact.SHA256, string(a.Store.Parsed)).Scan(&epoch)
	})
	if err != nil {
		return err
	}
	if epoch == nil || *epoch < 1 {
		return &ImportRecipeUnsupported{Reason: "imported_mask_requires_new_confirmation"}
	}
	return ctx.Err()
}

var _ ImportConfirmedMaskAuthority = SQLImportConfirmedMaskAuthority{}

// Person selector admission must observe the same current decision even when
// reached through a direct MaskRef, prepared-closure replay, or later review.
func (a SQLImportConfirmedMaskAuthority) QualifyImportedMask(ctx context.Context, mask review.MaskRef, target v2input.SessionVersionBinding) error {
	return a.QualifyPreparedMaskConfirmation(ctx, mask, target)
}

type importPersonMaskConsentChain struct {
	current ImportMaskQualifier
	other   ImportMaskQualifier
}

func (c importPersonMaskConsentChain) QualifyImportedMask(ctx context.Context, mask review.MaskRef, target v2input.SessionVersionBinding) error {
	if c.current == nil || c.other == nil {
		return v2input.ErrInvalid
	}
	if !hasImportedPersonMaskContext(ctx, mask) {
		return &ImportRecipeUnsupported{Reason: "imported_mask_requires_new_confirmation"}
	}
	if err := c.current.QualifyImportedMask(ctx, mask, target); err != nil {
		return err
	}
	if err := c.other.QualifyImportedMask(ctx, mask, target); err != nil {
		return err
	}
	return c.current.QualifyImportedMask(ctx, mask, target)
}

var _ ImportMaskQualifier = SQLImportConfirmedMaskAuthority{}
var _ ImportMaskQualifier = importPersonMaskConsentChain{}
