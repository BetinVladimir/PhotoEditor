package delivery

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/exportpreset"
	"photoeditor/backend/internal/v2input"
)

// PrepareSelectedDelivery chooses admission only from durable validated catalog
// evidence. Neither mutable preset rereads nor protocol fallback select a path.
// Recipe selection is pinned before IO for both ordinary and watermark packages.
func (s Service) PrepareSelectedDelivery(ctx context.Context, who auth.Identity, tid uuid.UUID, request CreateRequest, key string, deps CreateDeliveryDependencies) (PreparedCreateDelivery, error) {
	if err := ctx.Err(); err != nil {
		return PreparedCreateDelivery{}, err
	}
	intent, err := FreezeCreateIntent(tid, who, request, key)
	if err != nil {
		return PreparedCreateDelivery{}, err
	}
	if err := checkCreateSigningPolicy(ctx, tid, intent.Request().Policy, deps.Signing); err != nil {
		return PreparedCreateDelivery{}, err
	}
	if !deliveryPoolsIsolated(s.DB, deps.Scheduler) || deps.Objects == nil || deps.Documents == nil || deps.Freezer == nil || len(deps.Bucket) < 3 || len(deps.Bucket) > 63 || deps.Limits.RAMBytes <= 0 || deps.Limits.ScratchBytes <= 0 || deps.Limits.OutputBytes < 8<<20 {
		return PreparedCreateDelivery{}, domain.NewError("DELIVERY_UNAVAILABLE", 503)
	}
	request = intent.Request()
	if request.Policy.MetadataPolicy != "strip" {
		if deps.MetadataPreparer == nil {
			return PreparedCreateDelivery{}, domain.NewError("DELIVERY_METADATA_UNAVAILABLE", 503)
		}
		if _, err = deps.MetadataPreparer.environment(); err != nil {
			return PreparedCreateDelivery{}, domain.NewError("DELIVERY_METADATA_UNAVAILABLE", 503)
		}
	}
	// An earlier ordinary selection may already have been promoted under the
	// idempotency lock. Resume that exact pending document without repinning.
	ordinary, ordinaryErr := s.RecoverCreate(ctx, who, tid, request, key)
	if ordinaryErr == nil && ordinary.Found {
		return s.PrepareDelivery(ctx, who, tid, request, key, deps)
	}
	if ordinaryErr != nil && domain.AsError(ordinaryErr).Code != "DELIVERY_PENDING_INVALID" {
		return PreparedCreateDelivery{}, ordinaryErr
	}
	// The selection-only preparer accepts decoded ordinary and watermark configs;
	// it confers no source pin, executable prepare proof or activation authority.
	pending, err := s.PrepareWatermarkCreatePending(ctx, who, tid, request, key, deps.Freezer)
	if err != nil {
		if domain.AsError(err).Code == "DELIVERY_PENDING_INVALID" {
			promoted, recoveryErr := s.RecoverCreate(ctx, who, tid, request, key)
			if recoveryErr == nil && promoted.Found {
				return s.PrepareDelivery(ctx, who, tid, request, key, deps)
			}
		}
		return PreparedCreateDelivery{}, err
	}
	if pending.Completed || !pending.Found {
		return PreparedCreateDelivery{}, domain.NewError("DELIVERY_CREATE_STALE", 409)
	}
	watermark, err := selectedCatalogUsesWatermark(pending.Pending.Catalog)
	if err != nil {
		return PreparedCreateDelivery{}, err
	}
	if watermark {
		return s.PreparePinnedWatermarkDelivery(ctx, who, tid, request, key, deps)
	}
	if err := s.promoteOrdinarySelection(ctx, who, tid, request, key, pending.Pending); err != nil {
		return PreparedCreateDelivery{}, err
	}
	return s.PrepareDelivery(ctx, who, tid, request, key, deps)
}

func selectedCatalogUsesWatermark(catalog CreateCatalog) (bool, error) {
	if len(catalog.Presets) < 1 || len(catalog.Presets) > 16 {
		return false, v2input.ErrInvalid
	}
	watermark := false
	seen := map[uuid.UUID]bool{}
	for _, preset := range catalog.Presets {
		if preset.ID == uuid.Nil || seen[preset.ID] {
			return false, v2input.ErrInvalid
		}
		seen[preset.ID] = true
		config, err := exportpreset.DecodeFrozenConfig(preset.ConfigJCS, preset.SHA256)
		if err != nil {
			return false, err
		}
		watermark = watermark || config.Watermark != nil
	}
	return watermark, nil
}
