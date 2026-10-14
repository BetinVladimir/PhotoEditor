package delivery

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2render"
)

type promotionTx struct {
	raw     []byte
	receipt []byte
	exact   bool
	calls   int
	args    []any
}

func (tx *promotionTx) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	tx.calls++
	tx.args = args
	if strings.Contains(query, "route='Delivery.Selection'") {
		return deliveryReadRow{raw: tx.raw}
	}
	if strings.Contains(query, "idempotency_records") {
		return deliveryReadRow{raw: tx.receipt}
	}
	return proofRow{accepted: tx.exact}
}

func TestSelectionPromotionBindsFrozenDocumentToSavedPins(t *testing.T) {
	i, session, catalog, items := createDocumentsFixture(t)
	who := auth.Identity{UserID: i.actor, SessionID: session}
	pin := CreateRecipePin{Ordinal: 0, Asset: uuid.New(), Binding: items[0].Binding, Request: v2render.RenderFreezeRequest{DocumentID: uuid.New(), ExpectedRevision: "2"}}
	raw, err := canonical.Marshal(CreateSelectionPending{SchemaVersion: 1, Tenant: i.tenant, Actor: i.actor, RequestSHA: i.requestSHA, Catalog: catalog, Pins: []CreateRecipePin{pin}})
	if err != nil {
		t.Fatal(err)
	}
	key := uuid.NewSHA1(i.identity.Run, []byte("delivery:recipe:0:"+pin.Asset.String())).String()
	id, err := v2render.RenderFreezeDocumentID(i.tenant, who.UserID, pin.Asset, key, pin.Request)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := canonical.Marshal(v2render.RenderFreezeResult{DocumentID: id, DraftDocumentID: pin.Request.DocumentID, DraftRevision: pin.Request.ExpectedRevision, Source: pin.Binding, Recipe: items[0].Recipe})
	if err != nil {
		t.Fatal(err)
	}
	tx := &promotionTx{raw: raw, receipt: receipt, exact: true}
	got, err := i.promotionCatalog(context.Background(), tx, who, items)
	if err != nil || got.Snapshot != catalog.Snapshot || tx.calls != 3 {
		t.Fatalf("promotion: calls=%d error=%v", tx.calls, err)
	}
	if len(tx.args) != 6 || tx.args[0] != i.tenant || tx.args[1] != i.request.ProjectID || tx.args[2] != pin.Binding.AssetVersionID || tx.args[4] != items[0].Recipe.ArtifactID || tx.args[5] != items[0].Recipe.SHA256 {
		t.Fatal("recipe scope changed")
	}
	tx.exact = false
	if _, err := i.promotionCatalog(context.Background(), tx, who, items); err == nil {
		t.Fatal("unregistered document accepted")
	}
	tx.exact = true
	tx.receipt = []byte(`{}`)
	tx.calls = 0
	if _, err := i.promotionCatalog(context.Background(), tx, who, items); err == nil || tx.calls != 2 {
		t.Fatal("invalid freeze receipt reached upload lookup")
	}
	tx.calls = 0
	items[0].Binding.AssetVersionID = uuid.New()
	if _, err := i.promotionCatalog(context.Background(), tx, who, items); err == nil || tx.calls != 1 {
		t.Fatal("changed source reached upload lookup")
	}
}
