package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func publishedPackageFixture(tenant, id uuid.UUID) map[string]any {
	manifest := uuid.New()
	return map[string]any{"id": id, "revision": "3", "document": v2input.Ref{ArtifactID: manifest, SHA256: strings.Repeat("a", 64)}, "data": deliveryDataFixture(tenant,id,"3","ready",manifest,time.Now().UTC().Format(time.RFC3339Nano))}
}
func TestDeliveryPublicationReceiptExactScopeAndTransition(t *testing.T) {
	for _, mode := range []string{"valid", "id", "tenant", "data_id", "revision", "data_revision", "type", "state", "unpublished", "zero_time", "manifest", "document", "sha"} {
		t.Run(mode, func(t *testing.T) {
			tenant, id := uuid.New(), uuid.New()
			wire := publishedPackageFixture(tenant, id)
			data := wire["data"].(map[string]any)
			switch mode {
			case "id":
				wire["id"] = uuid.New()
			case "tenant":
				data["tenant_id"] = uuid.New()
			case "data_id":
				data["id"] = uuid.New()
			case "revision":
				wire["revision"] = "4"
			case "data_revision":
				data["revision"] = "4"
			case "type":
				data["resource_type"] = "interchange_jobs"
			case "state":
				data["state"] = "review"
			case "unpublished":
				data["published_at"] = nil
			case "zero_time":
				data["published_at"] = "0001-01-01T00:00:00Z"
			case "manifest":
				data["manifest_artifact_id"] = uuid.New()
			case "document":
				wire["document"] = nil
			case "sha":
				wire["document"] = v2input.Ref{ArtifactID: data["manifest_artifact_id"].(uuid.UUID), SHA256: "bad"}
			}
			raw, err := json.Marshal(wire)
			if err != nil {
				t.Fatal(err)
			}
			if err = validatePublishedPackage(raw, tenant, id, 3); (err == nil) != (mode == "valid") {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
		})
	}
}

type publicationReceiptTx struct {
	t           *testing.T
	tenant, id  uuid.UUID
	who         auth.Identity
	raw         []byte
	publication *uuid.UUID
	calls       int
	failure     error
	cancel      context.CancelFunc
}

func (tx *publicationReceiptTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	tx.calls++
	if tx.calls == 1 {
		if !strings.Contains(query, "publish_v30_delivery") || len(args) != 6 || args[0] != tx.tenant || args[1] != tx.id || args[2] != tx.who.UserID || args[3] != tx.who.SessionID || args[4] != int64(2) || args[5] != uuid.NewSHA1(tx.id, []byte("delivery:publish:"+tx.who.UserID.String()+":stable-key")) {
			tx.t.Fatal("publication scope/request identity changed")
		}
		return recoveryRow{scan: func(dest ...any) error {
			if tx.failure != nil {
				return tx.failure
			}
			*dest[0].(**uuid.UUID) = tx.publication
			if tx.cancel != nil {
				tx.cancel()
			}
			return nil
		}}
	}
	if tx.calls != 2 || !strings.Contains(query, "get_v30_delivery") {
		tx.t.Fatal("unexpected read")
	}
	return deliveryReadRow{raw: tx.raw}
}
func TestDeliveryPublishRequiresMatchingReceiptAfterSQL(t *testing.T) {
	for _, mode := range []string{"valid", "stale", "sql_error", "cancelled", "wrong_receipt"} {
		t.Run(mode, func(t *testing.T) {
			tenant, id, publication := uuid.New(), uuid.New(), uuid.New()
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			raw, err := json.Marshal(publishedPackageFixture(tenant, id))
			if err != nil {
				t.Fatal(err)
			}
			tx := &publicationReceiptTx{t: t, tenant: tenant, id: id, who: who, publication: &publication, raw: raw}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sentinel := errors.New("database refused")
			switch mode {
			case "stale":
				tx.publication = nil
			case "sql_error":
				tx.failure = sentinel
			case "cancelled":
				tx.cancel = cancel
			case "wrong_receipt":
				tx.raw = []byte(`{"id":"00000000-0000-0000-0000-000000000000"}`)
			}
			result, err := PublishPackage(ctx, tx, who, tenant, id, PublishRequest{ExpectedRevision: "2"}, "stable-key")
			if (err == nil) != (mode == "valid") {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
			if err != nil && result != nil {
				t.Fatal("failed publication returned resource")
			}
			if mode == "sql_error" && !errors.Is(err, sentinel) {
				t.Fatal("lost SQL error")
			}
			expected := 1
			if mode == "valid" || mode == "wrong_receipt" {
				expected = 2
			}
			if tx.calls != expected {
				t.Fatalf("queries=%d want=%d", tx.calls, expected)
			}
			if mode == "valid" {
				result[0] = '!'
				if tx.raw[0] == '!' {
					t.Fatal("receipt aliases SQL buffer")
				}
			}
		})
	}
}

func TestDeliveryPublicationCancelledBeforeDependencyAccess(t *testing.T) {
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 receipt,err:=(Service{}).PublishWithReceipt(ctx,auth.Identity{},uuid.New(),uuid.New(),PublishRequest{ExpectedRevision:"2"},"stable-key")
 if !errors.Is(err,context.Canceled)||len(receipt.Body)!=0{t.Fatal("cancelled publication returned receipt or dependency error",receipt,err)}
}
