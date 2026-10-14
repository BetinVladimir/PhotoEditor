package delivery

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2render"
)

func TestSelectionPendingBindsCatalogPinsActorAndIntent(t *testing.T) {
	i, _, catalog, items := createDocumentsFixture(t)
	pin := CreateRecipePin{Ordinal: 0, Asset: uuid.New(), Binding: items[0].Binding, Request: v2render.RenderFreezeRequest{DocumentID: uuid.New(), ExpectedRevision: "4"}}
	for _, kind := range []string{"valid", "tenant", "actor", "request", "snapshot", "count", "config", "revision", "ordinal", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			saved := CreateSelectionPending{SchemaVersion: 1, Tenant: i.tenant, Actor: i.actor, RequestSHA: i.requestSHA, Catalog: catalog, Pins: []CreateRecipePin{pin}}
			saved.Catalog.Presets = append([]CreateCatalogPreset(nil), catalog.Presets...)
			switch kind {
			case "tenant":
				saved.Tenant = uuid.New()
			case "actor":
				saved.Actor = uuid.New()
			case "request":
				saved.RequestSHA = digest([]byte("other"))
			case "snapshot":
				saved.Catalog.Snapshot = uuid.New()
			case "count":
				saved.Catalog.ItemCount++
			case "config":
				saved.Catalog.Presets[0].ConfigJCS = []byte(`{}`)
			case "revision":
				saved.Pins[0].Request.ExpectedRevision = "04"
			case "ordinal":
				saved.Pins[0].Ordinal = 1
			}
			raw, err := canonical.Marshal(saved)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "unknown" {
				raw = []byte(`{"unknown":true}`)
			}
			got, err := i.decodeSelectionPending(context.Background(), raw)
			if (err == nil) != (kind == "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
			if kind == "valid" && (got.Pins[0].Request != pin.Request || got.Pins[0].Binding != pin.Binding) {
				t.Fatal("pin changed")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := i.decodeSelectionPending(ctx, []byte(`{}`)); err != context.Canceled {
		t.Fatalf("cancel: %v", err)
	}
}
