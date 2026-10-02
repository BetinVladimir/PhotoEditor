package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// CheckUserMask proves a version-scoped user upload is an actual mask root in
// these original-source params. Scope alone cannot prove MaskRef bytes, geometry
// or binary probabilities; the declared pinned closure must still be verified.
func (s SQLOriginalCompositionPreflight) CheckUserMask(ctx context.Context, tenant, project, snapshot uuid.UUID, who auth.Identity, params json.RawMessage, mask v2input.Ref) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || tenant == uuid.Nil || project == uuid.Nil || snapshot == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || !validLayerArtifactRef(mask) || len(params) < 2 || len(params) > 1048576 {
		return v2input.ErrInvalid
	}
	body, err := canonical.Bytes(params)
	if err != nil || !bytes.Equal(body, params) {
		return v2input.ErrInvalid
	}
	return database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.v25_layer_original_user_mask_scope($1,$2,$3,$4,$5,$6::jsonb,$7,$8)`, tenant, project, snapshot, who.UserID, who.SessionID, json.RawMessage(body), mask.ArtifactID, mask.SHA256).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return v2input.ErrExpired
		}
		return nil
	})
}

// CheckUserMaskPixels consumes a durable publication binding, then repeats live
// scheduler scope. Atomic graph registration must invoke this proof again.
func (s SQLOriginalCompositionPreflight) CheckUserMaskPixels(ctx context.Context, tenant, project, snapshot uuid.UUID, who auth.Identity, params json.RawMessage, mask, pixels v2input.Ref, width, height int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || tenant == uuid.Nil || project == uuid.Nil || snapshot == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || !validLayerArtifactRef(mask) || !validLayerArtifactRef(pixels) || width < 1 || width > 200000000 || height < 1 || height > 200000000/width || len(params) < 2 || len(params) > 1048576 {
		return v2input.ErrInvalid
	}
	body, err := canonical.Bytes(params)
	if err != nil || !bytes.Equal(body, params) {
		return v2input.ErrInvalid
	}
	return database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.v25_layer_original_user_mask_pixels_scope($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10,$11,$12)`, tenant, project, snapshot, who.UserID, who.SessionID, json.RawMessage(body), mask.ArtifactID, mask.SHA256, pixels.ArtifactID, pixels.SHA256, width, height).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return v2input.ErrExpired
		}
		return nil
	})
}

// CheckUserMaskSet proves every nested and protected mask for new-user inputs.
// Adopted producer masks require their distinct producer-lineage admission.
func (s SQLOriginalCompositionPreflight) CheckUserMaskSet(ctx context.Context, tenant, project, snapshot uuid.UUID, who auth.Identity, params json.RawMessage, width, height int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.DB == nil || tenant == uuid.Nil || project == uuid.Nil || snapshot == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || width < 1 || width > 200000000 || height < 1 || height > 200000000/width || len(params) < 2 || len(params) > 1048576 {
		return v2input.ErrInvalid
	}
	body, err := canonical.Bytes(params)
	if err != nil || !bytes.Equal(body, params) {
		return v2input.ErrInvalid
	}
	return database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_scheduler" {
			return v2input.ErrInvalid
		}
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.v25_layer_original_user_mask_set_scope($1,$2,$3,$4,$5,$6::jsonb,$7,$8)`, tenant, project, snapshot, who.UserID, who.SessionID, json.RawMessage(body), width, height).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return v2input.ErrExpired
		}
		return nil
	})
}
