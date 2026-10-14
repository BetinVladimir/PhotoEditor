package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2optical"
)

// VerifyAdoptedMultiOriginalArrays streams every source array occurrence, including
// shared refs with different expected shapes. A saved inventory is never authority.
// This conservative implementation reloads the full inventory around each read;
// callers must not replace those checks with cached metadata. It grants no Apply.
func VerifyAdoptedMultiOriginalArrays(ctx context.Context, source AdoptedMultiOriginalInventorySource, objects v2input.SessionAnalysisObjectReader, who auth.Identity, tenant, project, producer uuid.UUID, originals VerifiedMultiOriginals) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	inv := originals.Inventory()
	binding := originals.binding
	if source == nil || objects == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenant == uuid.Nil || project == uuid.Nil {
		return v2input.ErrInvalid
	}
	if err := validateMultiOriginalInventory(inv, binding, producer, inv.bucket); err != nil {
		return err
	}
	specs, err := v2optical.MultiSourceArrays(binding.Operation, []byte(binding.ParamsJCS), originals.Documents())
	if err != nil {
		return err
	}
	images, err := originals.Images()
	if err != nil {
		return err
	}
	frames, _, _, err := binding.Inputs()
	if err != nil {
		return err
	}
	if err := verifyMultiOriginalInventoryEdges(inv, frames, images); err != nil {
		return err
	}
	type edge struct {
		ordinal int
		role    string
	}
	indexed := make(map[edge]MultiOriginalObject, len(inv.items))
	for _, item := range inv.items {
		indexed[edge{item.Ordinal, item.Role}] = item
	}
	current := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		fresh, err := source.LoadOriginalInventory(ctx, who, tenant, project, producer, binding, inv.bucket)
		if err != nil {
			return err
		}
		if !inv.equal(fresh) {
			return v2input.ErrExpired
		}
		return ctx.Err()
	}
	for _, spec := range specs {
		item, ok := indexed[edge{spec.Ordinal, spec.Array.Role}]
		if !ok || item.Ref != spec.Array.Ref {
			return v2input.ErrInvalid
		}
		if err := current(); err != nil {
			return err
		}
		reader, err := objects.Open(ctx, item.Object)
		if err != nil {
			if reader != nil {
				_ = reader.Close()
			}
			return err
		}
		if reader == nil {
			return v2input.ErrInvalid
		}
		verifyErr := v2optical.VerifyPerspectiveSourceArray(ctx, spec.Array, item.Object, reader)
		closeErr := reader.Close()
		if verifyErr != nil {
			return verifyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if err := current(); err != nil {
			return err
		}
	}
	return current()
}
