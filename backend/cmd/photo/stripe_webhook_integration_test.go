package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	stripe "github.com/stripe/stripe-go/v87"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/config"
)

func TestIntegrationStripeWebhookInstallerLeastPrivilege(t *testing.T) {
	dsn, raw := os.Getenv("PHOTO_TEST_DATABASE_URL"), os.Getenv("PHOTO_BILLING_STRIPE_INBOX_FIXTURE")
	if dsn == "" || raw == "" {
		t.Skip("isolated Stripe installation fixture required")
	}
	var fixture struct {
		Account  uuid.UUID `json:"account_id"`
		External string    `json:"external_account"`
		Version  string    `json:"api_version"`
	}
	if json.Unmarshal([]byte(raw), &fixture) != nil || fixture.Account == uuid.Nil {
		t.Fatal("invalid isolated fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("isolated database unavailable")
	}
	t.Cleanup(admin.Close)
	login := "stripe_ingress_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	password, err := auth.NewSecret(rand.Reader)
	if err != nil {
		t.Fatal("fixture entropy unavailable")
	}
	var ddl string
	if err = admin.QueryRow(ctx, "SELECT format('CREATE ROLE %I LOGIN PASSWORD %L', $1::text,$2::text)", login, password).Scan(&ddl); err != nil {
		t.Fatal("fixture login formatting failed")
	}
	if _, err = admin.Exec(ctx, ddl); err != nil {
		t.Fatal("fixture login creation failed")
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanup, "DROP ROLE "+pgx.Identifier{login}.Sanitize()); err != nil {
			t.Error("fixture role cleanup failed")
		}
	})
	if _, err = admin.Exec(ctx, "GRANT photo_maintenance TO "+pgx.Identifier{login}.Sanitize()); err != nil {
		t.Fatal("fixture role grant failed")
	}
	connection, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("fixture connection invalid")
	}
	connection.ConnConfig.User = login
	connection.ConnConfig.Password = password
	loginURL := url.URL{Scheme: "postgres", User: url.UserPassword(login, password), Host: net.JoinHostPort(connection.ConnConfig.Host, strconv.Itoa(int(connection.ConnConfig.Port))), Path: "/" + connection.ConnConfig.Database, RawQuery: "sslmode=disable"}
	loginDSN := loginURL.String()
	directory := t.TempDir()
	signingPath, keyPath := filepath.Join(directory, "signing"), filepath.Join(directory, "payload")
	const signingSecret = "whsec_fixtureonly"
	if err = os.WriteFile(signingPath, []byte(signingSecret), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(keyPath, []byte(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := config.StripeIngressConfig{Enabled: true, DatabaseURL: loginDSN, ProviderAccountID: fixture.Account, AccountID: fixture.External, APIVersion: fixture.Version, WebhookSecretRef: signingPath, PayloadKeyRef: keyPath, PayloadKeyID: "fixture-v1"}
	service, closeService, err := installStripeWebhookIngress(ctx, cfg)
	if err != nil {
		t.Fatal("least-privilege installer failed", err)
	}
	defer closeService()
	body := []byte(`{"id":"evt_runtimefixture","object":"event","type":"invoice.paid","api_version":"2026-10-28.endive","livemode":false,"created":1793491200,"data":{"object":{"id":"in_fixture"}}}`)
	header := stripe.GenerateTestSignedPayload(&stripe.UnsignedPayload{Payload: body, Secret: signingSecret}).Header
	received, err := service.ReceiveStripeWebhook(ctx, fixture.Account, body, header)
	if err != nil || !received.Received {
		t.Fatal("installed receiver failed", err)
	}
	var count int
	if err = admin.QueryRow(ctx, "SELECT count(*) FROM photo.billing_webhook_inbox WHERE event_id='evt_runtimefixture'").Scan(&count); err != nil || count != 1 {
		t.Fatal("durable event missing")
	}
	// Startup refuses a privileged login even if SET ROLE would name maintenance.
	cfg.DatabaseURL = dsn
	invalid, closeInvalid, err := installStripeWebhookIngress(ctx, cfg)
	if closeInvalid != nil {
		closeInvalid()
	}
	if err == nil || invalid != nil {
		t.Fatal("privileged runtime database login accepted")
	}
	cfg.DatabaseURL = loginDSN
	cfg.AccountID = "acct_other"
	invalid, closeInvalid, err = installStripeWebhookIngress(ctx, cfg)
	if closeInvalid != nil {
		closeInvalid()
	}
	if err == nil || invalid != nil {
		t.Fatal("wrong account mapping accepted")
	}
}
