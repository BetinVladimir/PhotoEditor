package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type selectedRecipe struct {
	Version uuid.UUID
	Ref     v2input.Ref
	Object  objectstore.Object
}

func (s SQLTransferSourceReader) recipeObjects(ctx context.Context, who auth.Identity, request Request, inventory CatalogInventory) ([]selectedRecipe, error) {
	var result []selectedRecipe
	err := tenant.WithTenant(ctx, s.DB, who, request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		input := request.Intent
		rows, err := tx.Query(ctx, "SELECT ordinal,asset_version_id,artifact_id,sha256,bytes,bucket,object_key,object_version,media_type FROM photo.semantic_proposal_recipe_objects($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", request.TenantID, who.UserID, who.SessionID, input.Reference.AssetVersionID, input.Reference.Source.ArtifactID, input.Reference.Source.SHA256, input.Reference.CanvasHash, input.ReferenceMask.ArtifactID, input.ReferenceMask.SHA256, input.TargetSnapshotID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var row selectedRecipe
			var ordinal int
			var bucket string
			if err := rows.Scan(&ordinal, &row.Version, &row.Ref.ArtifactID, &row.Ref.SHA256, &row.Object.Bytes, &bucket, &row.Object.Key, &row.Object.VersionID, &row.Object.ContentType); err != nil {
				return err
			}
			if ordinal != len(result) || ordinal >= len(inventory.Targets) || row.Version != inventory.Targets[ordinal].AssetVersionID || bucket != s.Bucket {
				return v2input.ErrInvalid
			}
			row.Object.SHA256 = row.Ref.SHA256
			result = append(result, row)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(result) != len(inventory.Targets) {
			return v2input.ErrConflict
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ReadTargetSnapshot verifies selected recipe bytes outside SQL and repeats
// inventory and current-document pins afterward. Admission must lock them again.
func (s SQLTransferSourceReader) ReadTargetSnapshot(ctx context.Context, who auth.Identity, request Request) (v2input.SessionInputSnapshot, []byte, error) {
	var empty v2input.SessionInputSnapshot
	if err := ctx.Err(); err != nil {
		return empty, nil, err
	}
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || who.UserID != request.ActorID {
		return empty, nil, v2input.ErrInvalid
	}
	inventory, err := ReadInventory(ctx, s.DB, who, request)
	if err != nil {
		return empty, nil, err
	}
	if inventory.Scope.ProjectID != s.ProjectID {
		return empty, nil, v2input.ErrInvalid
	}
	pins, err := s.recipeObjects(ctx, who, request, inventory)
	if err != nil {
		return empty, nil, err
	}
	recipes := make([]SnapshotRecipe, len(pins))
	for i, pin := range pins {
		body, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, pin.Object, pin.Ref)
		if err != nil {
			return empty, nil, err
		}
		recipes[i] = SnapshotRecipe{Ref: pin.Ref, Body: body}
	}
	snapshot, body, err := CompileTargetSnapshot(request.Intent.TargetSnapshotID, inventory, recipes)
	if err != nil {
		return empty, nil, err
	}
	after, err := ReadInventory(ctx, s.DB, who, request)
	if err != nil {
		return empty, nil, err
	}
	if !sameTransferInventory(inventory, after) {
		return empty, nil, v2input.ErrConflict
	}
	next, err := s.recipeObjects(ctx, who, request, after)
	if err != nil {
		return empty, nil, err
	}
	if len(next) != len(pins) {
		return empty, nil, v2input.ErrConflict
	}
	for i := range pins {
		if next[i] != pins[i] {
			return empty, nil, v2input.ErrConflict
		}
	}
	return snapshot, body, nil
}
