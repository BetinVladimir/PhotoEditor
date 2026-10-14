package delivery

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2render"
)

func TestDeliveryDraftSelectionCaptureScopeAndInvalidInventory(t *testing.T) {
	i, session, _, items := createDocumentsFixture(t)
	who := auth.Identity{UserID: i.actor, SessionID: session}
	pin := CreateRecipePin{Ordinal: 0, Asset: uuid.New(), Binding: items[0].Binding, Request: v2render.RenderFreezeRequest{DocumentID: uuid.New(), ExpectedRevision: "2"}}
	raw, err := canonical.Marshal([]CreateRecipePin{pin})
	if err != nil {
		t.Fatal(err)
	}
	tx := &deliveryReadTx{raw: raw}
	pins, err := i.CaptureDraftSelection(context.Background(), tx, who, i.tenant)
	if err != nil || len(pins) != 1 || pins[0].Binding != pin.Binding || pins[0].Request != pin.Request {
		t.Fatalf("capture: %v", err)
	}
	if len(tx.args) != 5 || tx.args[0] != i.tenant || tx.args[1] != i.request.ProjectID || tx.args[2] != i.request.SnapshotID || tx.args[3] != who.UserID || tx.args[4] != session {
		t.Fatal("capture scope")
	}
	for _, kind := range []string{"null", "empty", "ordinal", "revision", "duplicate", "source", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			changed := pin
			list := []CreateRecipePin{changed}
			switch kind {
			case "ordinal":
				list[0].Ordinal = 1
			case "revision":
				list[0].Request.ExpectedRevision = "02"
			case "duplicate":
				changed.Ordinal = 1
				list = append(list, changed)
			case "source":
				list[0].Binding.Source.ArtifactID = uuid.Nil
			}
			body, err := canonical.Marshal(list)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "null" {
				body = []byte("null")
			}
			if kind == "empty" {
				body = []byte("[]")
			}
			if kind == "unknown" {
				body = []byte(`[{"unknown":true}]`)
			}
			if _, err := i.CaptureDraftSelection(context.Background(), &deliveryReadTx{raw: body}, who, i.tenant); err == nil {
				t.Fatal("invalid captured inventory accepted")
			}
		})
	}
	tx.calls = 0
	if _, err := i.CaptureDraftSelection(context.Background(), tx, who, uuid.New()); err == nil || tx.calls != 0 {
		t.Fatal("foreign tenant queried")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := i.CaptureDraftSelection(ctx, tx, who, i.tenant); err != context.Canceled || tx.calls != 0 {
		t.Fatal("cancelled query")
	}
}
