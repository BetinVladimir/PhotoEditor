package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestLegacyPendingRestoresOnlyAsSelectionUntilRevalidatedPromotion(t *testing.T) {
	intent, session, catalog, items := createDocumentsFixture(t)
	raw, err := intent.EncodePending(context.Background(), session, catalog, items)
	if err != nil { t.Fatal(err) }
	var legacy map[string]any
	if err := json.Unmarshal(raw, &legacy); err != nil { t.Fatal(err) }
	delete(legacy, "stage")
	raw, err = canonical.Marshal(legacy)
	if err != nil { t.Fatal(err) }
	if _, err := intent.DecodePending(context.Background(), session, raw); err == nil { t.Fatal("legacy ambiguity executed") }
	selection, err := intent.decodeRecoveryPending(context.Background(), session, raw, false, true)
	if err != nil || selection.Stage != "" { t.Fatal("legacy recovery lost", err) }
	// Recompilation validates the current complete executable plan; production
	// promotion additionally checks current SQL source/preset authority under lock.
	promoted, err := intent.EncodePending(context.Background(), session, selection.Catalog, selection.Items)
	if err != nil { t.Fatal(err) }
	if _, err := intent.DecodePending(context.Background(), session, promoted); err != nil { t.Fatal(err) }
	if _, err := intent.DecodeWatermarkSelectionPending(context.Background(), session, promoted); err == nil { t.Fatal("executable reclassified as selection") }
}

func TestOrdinarySelectedPendingRequiresExplicitPromotion(t *testing.T) {
	intent, session, catalog, items := createDocumentsFixture(t)
	selected, err := intent.EncodeWatermarkSelectionPending(context.Background(), session, catalog, items)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := intent.DecodeWatermarkSelectionPending(context.Background(), session, selected); err != nil {
		t.Fatal(err)
	}
	if _, err := intent.DecodePending(context.Background(), session, selected); err == nil {
		t.Fatal("selection-only evidence accepted as executable ordinary pending")
	}
	promoted, err := intent.EncodePending(context.Background(), session, catalog, items)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := intent.DecodePending(context.Background(), session, promoted); err != nil {
		t.Fatal("ordinary promotion refused", err)
	}
	again, err := intent.EncodePending(context.Background(), session, catalog, items)
	if err != nil || !bytes.Equal(promoted, again) || bytes.Equal(selected, promoted) {
		t.Fatal("ordinary promotion is not an exact idempotent transition", err)
	}
}

func TestSelectedCreateCatalogRequiresCompleteValidFrozenConfigs(t *testing.T) {
	_, _, catalog, _ := createDocumentsFixture(t)
	watermark, err := selectedCatalogUsesWatermark(catalog)
	if err != nil || watermark {
		t.Fatal("ordinary selection", watermark, err)
	}
	_, config, _ := watermarkPinFixture(t)
	changed := catalog
	changed.Presets = append([]CreateCatalogPreset(nil), catalog.Presets...)
	changed.Presets[0].ConfigJCS = append([]byte(nil), config...)
	changed.Presets[0].SHA256 = digest(config)
	if watermark, err = selectedCatalogUsesWatermark(changed); err != nil || !watermark {
		t.Fatal("watermark selection", watermark, err)
	}
	changed.Presets = append(changed.Presets, changed.Presets[0])
	if _, err = selectedCatalogUsesWatermark(changed); err == nil {
		t.Fatal("duplicate preset accepted")
	}
	changed = catalog
	changed.Presets = append([]CreateCatalogPreset(nil), catalog.Presets...)
	changed.Presets[0].SHA256 = digest([]byte("changed"))
	if _, err = selectedCatalogUsesWatermark(changed); err == nil {
		t.Fatal("bad config accepted")
	}
	if _, err = selectedCatalogUsesWatermark(CreateCatalog{}); err == nil {
		t.Fatal("empty selection accepted")
	}
}
func TestSelectedCreatePipelinePreflight(t *testing.T) {
	intent, session, _, _ := createDocumentsFixture(t)
	who := auth.Identity{UserID: intent.actor, SessionID: session}
	if _, err := (Service{}).PrepareSelectedDelivery(context.Background(), who, intent.tenant, intent.Request(), intent.key, CreateDeliveryDependencies{}); err == nil {
		t.Fatal("missing dependencies accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Service{}).PrepareSelectedDelivery(ctx, who, intent.tenant, intent.Request(), intent.key, CreateDeliveryDependencies{}); err != context.Canceled {
		t.Fatal("cancel ignored", err)
	}
}
