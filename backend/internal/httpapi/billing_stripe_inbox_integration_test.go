package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	stripe "github.com/stripe/stripe-go/v87"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/billing"
	"photoeditor/backend/internal/billingprovider"
	"photoeditor/backend/internal/config"
)

func TestIntegrationSignedStripeDurableInbox(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	fixtureRaw := os.Getenv("PHOTO_BILLING_STRIPE_INBOX_FIXTURE")
	if dsn == "" || fixtureRaw == "" {
		t.Skip("isolated Stripe inbox fixture required")
	}
	var fixture struct {
		Account  uuid.UUID `json:"account_id"`
		External string    `json:"external_account"`
		Version  string    `json:"api_version"`
	}
	if json.Unmarshal([]byte(fixtureRaw), &fixture) != nil || fixture.Account == uuid.Nil {
		t.Fatal("invalid isolated fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_maintenance"
	cfg.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var role string
	if err = pool.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil || role != "photo_maintenance" {
		t.Fatal("incorrect ingest role", role, err)
	}
	keys := auth.KeyRing{Active: "fixture-v1", Keys: map[string][]byte{"fixture-v1": bytes.Repeat([]byte{7}, 32)}}
	store, err := billingprovider.NewPostgresWebhookRecorder(pool, fixture.Account, fixture.External, fixture.Version, keys)
	if err != nil {
		t.Fatal(err)
	}
	parser, err := billingprovider.NewStripeEventParser(fixture.External, fixture.Version)
	if err != nil {
		t.Fatal(err)
	}
	const secret = "stripe-signature-fixture-only"
	ingress := billingprovider.NewWebhookIngress(parser, store, secret)
	handler := (&Server{BillingWebhook: billing.StripeWebhookReceiver{Ingress: ingress, ProviderAccountID: fixture.Account, ExternalAccountID: fixture.External}, Config: config.Config{Environment: "development", MaxJSONBytes: 1048576}}).Routes()
	eventID := "evt_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	body := []byte(fmt.Sprintf(`{"id":%q,"object":"event","type":"invoice.paid","api_version":%q,"livemode":false,"created":1793491200,"data":{"object":{"id":"in_fixture","amount":10}}}`, eventID, fixture.Version))
	sign := func(raw []byte) string {
		return stripe.GenerateTestSignedPayload(&stripe.UnsignedPayload{Payload: raw, Secret: secret}).Header
	}
	send := func(account uuid.UUID, raw []byte, signature string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/stripe/"+account.String(), bytes.NewReader(raw)).WithContext(ctx)
		req.Header.Set("Stripe-Signature", signature)
		req.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		return out
	}
	count := func(table string) int {
		var n int
		if err := admin.QueryRow(ctx, "SELECT count(*) FROM photo."+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if out := send(fixture.Account, body, sign(body)+"0"); out.Code != 400 || count("billing_webhook_inbox") != 0 {
		t.Fatal("bad signature persisted", out.Code)
	}
	if out := send(uuid.New(), body, sign(body)); out.Code != 404 || count("billing_webhook_inbox") != 0 {
		t.Fatal("wrong account persisted", out.Code)
	}
	var wg sync.WaitGroup
	for n := 0; n < 16; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if out := send(fixture.Account, body, sign(body)); out.Code != 200 || out.Body.String() != "{\"received\":true}\n" {
				t.Errorf("duplicate delivery: %d %s", out.Code, out.Body.String())
			}
		}()
	}
	wg.Wait()
	if count("billing_webhook_inbox") != 1 {
		t.Fatal("duplicate durable records")
	}
	var encrypted, hash []byte
	var persistedVersion string
	if err = admin.QueryRow(ctx, "SELECT payload_encrypted,payload_hash,received_api_version FROM photo.billing_webhook_inbox WHERE event_id=$1", eventID).Scan(&encrypted, &hash, &persistedVersion); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, body) || persistedVersion != fixture.Version {
		t.Fatal("raw payload stored or version lost")
	}
	expected := sha256.Sum256(body)
	if !bytes.Equal(hash, expected[:]) {
		t.Fatal("wrong immutable hash")
	}
	plain, err := billingprovider.OpenWebhookPayload(keys, fixture.Account, fixture.External, eventID, hash, encrypted)
	if err != nil || !bytes.Equal(plain, body) {
		t.Fatal("encrypted payload not recoverable", err)
	}
	if _, err = billingprovider.OpenWebhookPayload(keys, uuid.New(), fixture.External, eventID, hash, encrypted); err == nil {
		t.Fatal("cross-account decryption")
	}
	changed := bytes.Replace(body, []byte(`"amount":10`), []byte(`"amount":11`), 1)
	if out := send(fixture.Account, changed, sign(changed)); out.Code != 409 {
		t.Fatal("changed signed payload accepted", out.Code)
	}
	if count("billing_webhook_conflicts") != 1 {
		t.Fatal("conflict evidence rolled back")
	}
	var state string
	var after []byte
	if err = admin.QueryRow(ctx, "SELECT state,payload_encrypted FROM photo.billing_webhook_inbox WHERE event_id=$1", eventID).Scan(&state, &after); err != nil || state != "dead" || !bytes.Equal(encrypted, after) {
		t.Fatal("quarantine failed or original overwritten", state, err)
	}
	if count("billing_invoice_allocations") != 0 {
		t.Fatal("ingress granted entitlement")
	}
	signature := "photo.billing_ingest_stripe_webhook(uuid,uuid,text,text,text,text,text,timestamptz,bytea,bytea)"
	if _, err = admin.Exec(ctx, "REVOKE EXECUTE ON FUNCTION "+signature+" FROM photo_maintenance"); err != nil {
		t.Fatal(err)
	}
	next := bytes.Replace(body, []byte(eventID), []byte("evt_commitfailure"), 1)
	if out := send(fixture.Account, next, sign(next)); out.Code != 503 || count("billing_webhook_inbox") != 1 {
		t.Fatal("failed durable write acknowledged", out.Code)
	}
}
