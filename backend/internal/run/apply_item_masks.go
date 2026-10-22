package run

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/editor"
)

type ApplyItemMaskOverride struct {
	ItemID     uuid.UUID `json:"item_id"`
	OverrideID uuid.UUID `json:"overrides_id"`
}
type applyItemMaskInput struct {
	ItemID         uuid.UUID       `json:"item_id"`
	AssetVersionID uuid.UUID       `json:"asset_version_id"`
	InputSHA       string          `json:"input_sha256"`
	BodySHA        string          `json:"body_sha256"`
	Body           json.RawMessage `json:"body"`
}

// Only existing actor-owned immutable OverrideCreate receipts may enter a
// normal run; no client-supplied body/hash or qualified/accepted rows are used.
func loadApplyItemMasks(ctx context.Context, tx pgx.Tx, who auth.Identity, tid uuid.UUID, items []applySelection, requests []ApplyItemMaskOverride) ([]applyItemMaskInput, error) {
	if len(requests) == 0 {
		return nil, nil
	}
	if len(requests) != len(items) || len(requests) > 5000 {
		return nil, domain.NewError("OVERRIDE_SOURCE_MISMATCH", 409)
	}
	byID := map[uuid.UUID]applySelection{}
	for _, item := range items {
		byID[item.ItemID] = item
	}
	out := make([]applyItemMaskInput, 0, len(items))
	seen := map[uuid.UUID]bool{}
	for _, request := range requests {
		item, exists := byID[request.ItemID]
		if !exists || seen[request.ItemID] || request.OverrideID == uuid.Nil {
			return nil, domain.NewError("OVERRIDE_SOURCE_MISMATCH", 409)
		}
		seen[request.ItemID] = true
		var input applyItemMaskInput
		input.ItemID = request.ItemID
		err := tx.QueryRow(ctx, `SELECT config,config_hash,asset_version_id,input_sha256 FROM photo.overrides WHERE tenant_id=$1 AND id=$2 AND created_by=$3`, tid, request.OverrideID, who.UserID).Scan(&input.Body, &input.BodySHA, &input.AssetVersionID, &input.InputSHA)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.NewError("NOT_FOUND", 404)
		}
		if err != nil {
			return nil, err
		}
		pin, err := canonical.Hash(json.RawMessage(input.Body))
		if err != nil || pin != input.BodySHA || input.AssetVersionID != item.AssetVersionID || input.InputSHA != item.SHA256 {
			return nil, domain.NewError("OVERRIDE_SOURCE_MISMATCH", 409)
		}
		var envelope struct {
			Masks   []json.RawMessage          `json:"mask_refs"`
			Modules map[string]json.RawMessage `json:"modules"`
		}
		if json.Unmarshal(input.Body, &envelope) != nil || len(envelope.Masks) == 0 || len(envelope.Masks) > 128 || len(envelope.Modules) != 1 || string(envelope.Modules["strength"]) != "1" {
			return nil, domain.NewError("OVERRIDE_MASK_INVALID", 409)
		}
		if err = editor.ValidateOverrideMaskSources(ctx, tx, who, tid, input.AssetVersionID, input.Body, input.InputSHA); err != nil {
			return nil, err
		}
		out = append(out, input)
	}
	return out, nil
}
