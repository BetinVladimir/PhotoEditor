package delivery

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
)

type recipeFreezerFunc func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, v2render.RenderFreezeRequest) (idempotency.Response, error)

func (f recipeFreezerFunc) Freeze(ctx context.Context, who auth.Identity, tid, asset uuid.UUID, key string, request v2render.RenderFreezeRequest) (idempotency.Response, error) {
	return f(ctx, who, tid, asset, key, request)
}

func TestDeliveryRecipeFreezePreflightReplayAndSourceBinding(t *testing.T) {
	i, session, _, items := createDocumentsFixture(t)
	who := auth.Identity{UserID: i.actor, SessionID: session}
	pins := []CreateRecipePin{{Ordinal: 0, Asset: uuid.New(), Binding: items[0].Binding, Request: v2render.RenderFreezeRequest{DocumentID: uuid.New(), ExpectedRevision: "3"}}}
	for _, mode := range []string{"success", "source", "status", "resource", "revision"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			var firstKey string
			freezer := recipeFreezerFunc(func(_ context.Context, actor auth.Identity, tid, asset uuid.UUID, key string, request v2render.RenderFreezeRequest) (idempotency.Response, error) {
				calls++
				if actor != who || tid != i.tenant || asset != pins[0].Asset || request != pins[0].Request {
					t.Fatal("freeze scope changed")
				}
				if firstKey != "" && firstKey != key {
					t.Fatal("unstable replay key")
				}
				firstKey = key
				id, err := v2render.RenderFreezeDocumentID(tid, who.UserID, asset, key, request)
				if err != nil {
					t.Fatal(err)
				}
				body := v2render.RenderFreezeResult{DocumentID: id, DraftDocumentID: request.DocumentID, DraftRevision: request.ExpectedRevision, Source: pins[0].Binding, Recipe: v2input.Ref{ArtifactID: id, SHA256: digest([]byte("frozen"))}}
				if mode == "source" {
					body.Source.AssetVersionID = uuid.New()
				}
				if mode == "revision" {
					body.DraftRevision = "4"
				}
				raw, err := canonical.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				out := idempotency.Response{Status: 200, Body: raw, ResourceID: &id}
				if mode == "status" {
					out.Status = 201
				}
				if mode == "resource" {
					out.ResourceID = nil
				}
				return out, nil
			})
			for attempt := 0; attempt < 2; attempt++ {
				refs, err := i.FreezeRecipeSelection(context.Background(), who, pins, freezer)
				if (err == nil) != (mode == "success") {
					t.Fatalf("%s: %v", mode, err)
				}
				if err != nil && refs != nil {
					t.Fatal("partial result escaped")
				}
			}
			if calls != 2 {
				t.Fatal("unexpected callback count")
			}
		})
	}
	bad := append([]CreateRecipePin(nil), pins...)
	bad = append(bad, pins[0])
	bad[1].Ordinal = 1
	calls := 0
	freezer := recipeFreezerFunc(func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, string, v2render.RenderFreezeRequest) (idempotency.Response, error) {
		calls++
		return idempotency.Response{}, nil
	})
	if _, err := i.FreezeRecipeSelection(context.Background(), who, bad, freezer); err == nil || calls != 0 {
		t.Fatal("invalid batch performed IO")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := i.FreezeRecipeSelection(ctx, who, pins, freezer); err != context.Canceled || calls != 0 {
		t.Fatal("cancel performed IO")
	}
}
