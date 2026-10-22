package collection

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"strconv"
)

type ItemVersionReplace struct {
	ExpectedRevision string    `json:"expected_revision"`
	ExpectedVersion  uuid.UUID `json:"expected_asset_version_id"`
	Version          uuid.UUID `json:"asset_version_id"`
}

func validateItemVersionReplace(req ItemVersionReplace) error {
	revision, err := strconv.ParseInt(req.ExpectedRevision, 10, 64)
	if err != nil || revision < 1 || strconv.FormatInt(revision, 10) != req.ExpectedRevision || req.Version == uuid.Nil || req.ExpectedVersion == uuid.Nil || req.Version == req.ExpectedVersion {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	return nil
}

// Replace only a direct accepted child of the expected version, in one CAS.
// Parallel collection edits conflict; retries must refresh instead of guessing.
func (s Service) ReplaceItemVersion(ctx context.Context, who auth.Identity, tid, collectionID, itemID uuid.UUID, req ItemVersionReplace) (Item, error) {
	var out Item
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if err := validateItemVersionReplace(req); err != nil {
		return out, err
	}
	if s.DB == nil || tid == uuid.Nil || collectionID == uuid.Nil || itemID == uuid.Nil {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		c, err := authorize(ctx, tx, who, tid, collectionID, role, domain.Editor, true)
		if err != nil {
			return err
		}
		if !writableCollectionState(c.State) || c.Revision != req.ExpectedRevision {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		current, err := scanItem(tx.QueryRow(ctx, `SELECT `+itemFields+` FROM photo.collection_items i WHERE i.tenant_id=$1 AND i.collection_id=$2 AND i.id=$3 FOR UPDATE`, tid, collectionID, itemID))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if err != nil {
			return err
		}
		if current.AssetVersionID != req.ExpectedVersion {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		var accepted bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM photo.asset_versions v JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id
   JOIN photo.artifacts artifact ON artifact.tenant_id=v.tenant_id AND artifact.id=v.master_artifact_id AND artifact.sha256=v.content_hash
   WHERE v.tenant_id=$1 AND v.id=$2 AND v.asset_id=$3 AND v.parent_asset_version_id=$4 AND v.review_state='accepted'
   AND a.project_id=$5 AND a.deleted_at IS NULL AND a.upload_state='verified'
   AND artifact.state='ready' AND artifact.verified_at IS NOT NULL AND artifact.object_version IS NOT NULL AND artifact.object_version NOT IN('','null')
   AND (artifact.expires_at IS NULL OR artifact.expires_at>clock_timestamp()))`, tid, req.Version, current.AssetID, req.ExpectedVersion, c.ProjectID).Scan(&accepted)
		if err != nil {
			return err
		}
		if !accepted {
			return domain.NewError("VALIDATION_FAILED", 422)
		}
		out, err = scanItem(tx.QueryRow(ctx, `UPDATE photo.collection_items AS i SET asset_version_id=$4 WHERE tenant_id=$1 AND collection_id=$2 AND id=$3 RETURNING `+itemFields, tid, collectionID, itemID, req.Version))
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE photo.collections SET revision=revision+1 WHERE tenant_id=$1 AND id=$2`, tid, collectionID); err != nil {
			return err
		}
		return audit(ctx, tx, who, tid, collectionID, "collection.item_version_replace")
	})
	return out, err
}

type ItemVersionState struct {
	Item     Item   `json:"item"`
	Revision string `json:"collection_revision"`
}

// The collection lock observes item and revision consistently with every writer.
func (s Service) ItemVersionState(ctx context.Context, who auth.Identity, tid, collectionID, itemID uuid.UUID) (ItemVersionState, error) {
	var out ItemVersionState
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if s.DB == nil || tid == uuid.Nil || collectionID == uuid.Nil || itemID == uuid.Nil {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		c, err := authorize(ctx, tx, who, tid, collectionID, role, domain.Editor, true)
		if err != nil {
			return err
		}
		out.Revision = c.Revision
		out.Item, err = scanItem(tx.QueryRow(ctx, `SELECT `+itemFields+` FROM photo.collection_items i WHERE i.tenant_id=$1 AND i.collection_id=$2 AND i.id=$3`, tid, collectionID, itemID))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		return err
	})
	return out, err
}
