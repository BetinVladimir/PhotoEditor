package billingprovider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
)

type webhookQuery func(context.Context, string, ...any) pgx.Row

func (f webhookQuery) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	return f(ctx, q, args...)
}

type webhookRecordRow struct {
	id          uuid.UUID
	quarantined bool
}

func (r webhookRecordRow) Scan(dest ...any) error {
	*(dest[0].(*uuid.UUID)) = r.id
	*(dest[1].(*bool)) = !r.quarantined
	*(dest[2].(*bool)) = r.quarantined
	return nil
}
func recordFixture() WebhookInboxEntry {
	return WebhookInboxEntry{ID: uuid.New(), AccountID: "acct_fixture", ProviderEventID: "evt_fixture", EventType: "invoice.paid", State: WebhookInboxStatePending, RawPayload: []byte(`{"id":"evt_fixture","object":"event","type":"invoice.paid","api_version":"2026-10-28.endive","livemode":false,"created":1793491200,"data":{"object":{"id":"in_fixture"}}}`)}
}
func TestPostgresWebhookRecorderEncryptsAndOwnsKeyMaterial(t *testing.T) {
	account := uuid.New()
	entry := recordFixture()
	var encrypted, hash []byte
	db := webhookQuery(func(_ context.Context, _ string, args ...any) pgx.Row {
		if len(args) != 10 || args[0] != entry.ID || args[1] != account || args[2] != "acct_fixture" || args[3] != "2026-10-28.endive" {
			t.Fatal("unbound inbox identity")
		}
		encrypted = bytes.Clone(args[8].([]byte))
		hash = bytes.Clone(args[9].([]byte))
		return webhookRecordRow{id: entry.ID}
	})
	rawKey := bytes.Repeat([]byte{7}, 32)
	keys := auth.KeyRing{Active: "fixture-v1", Keys: map[string][]byte{"fixture-v1": rawKey}}
	recorder, err := NewPostgresWebhookRecorder(db, account, "acct_fixture", "2026-10-28.endive", keys)
	if err != nil {
		t.Fatal(err)
	}
	rawKey[0] = 9
	created, err := recorder.Record(context.Background(), entry)
	if err != nil || !created {
		t.Fatal("record failed", err)
	}
	originalKeys := auth.KeyRing{Keys: map[string][]byte{"fixture-v1": bytes.Repeat([]byte{7}, 32)}}
	expected := sha256.Sum256(entry.RawPayload)
	if !bytes.Equal(hash, expected[:]) || bytes.Contains(encrypted, entry.RawPayload) {
		t.Fatal("wrong hash or plaintext stored")
	}
	raw, err := OpenWebhookPayload(originalKeys, account, "acct_fixture", "evt_fixture", hash, encrypted)
	if err != nil || !bytes.Equal(raw, entry.RawPayload) {
		t.Fatal("owned key did not decrypt", err)
	}
	for _, tc := range []struct {
		account         uuid.UUID
		external, event string
		hash, encrypted []byte
	}{
		{uuid.New(), "acct_fixture", "evt_fixture", hash, encrypted},
		{account, "acct_other", "evt_fixture", hash, encrypted},
		{account, "acct_fixture", "evt_other", hash, encrypted},
		{account, "acct_fixture", "evt_fixture", bytes.Repeat([]byte{0}, 32), encrypted},
		{account, "acct_fixture", "evt_fixture", hash, append(bytes.Clone(encrypted), []byte(`{}`)...)},
	} {
		if _, err := OpenWebhookPayload(originalKeys, tc.account, tc.external, tc.event, tc.hash, tc.encrypted); err == nil {
			t.Fatal("accepted ciphertext outside original binding")
		}
	}
}
func TestPostgresWebhookRecorderRejectsInvalidScopeBeforeDatabase(t *testing.T) {
	db := webhookQuery(func(context.Context, string, ...any) pgx.Row { t.Fatal("unexpected database call"); return nil })
	keys := auth.KeyRing{Active: "fixture", Keys: map[string][]byte{"fixture": bytes.Repeat([]byte{1}, 32)}}
	recorder, err := NewPostgresWebhookRecorder(db, uuid.New(), "acct_fixture", "2026-10-28.endive", keys)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ old, new string }{{`"livemode":false`, `"livemode":true`}, {`2026-10-28.endive`, `wrong-version`}, {`"id":"in_fixture"`, `"object":"invoice"`}} {
		entry := recordFixture()
		entry.RawPayload = bytes.Replace(entry.RawPayload, []byte(tc.old), []byte(tc.new), 1)
		if _, err := recorder.Record(context.Background(), entry); err == nil {
			t.Fatal("accepted invalid envelope")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := recorder.Record(ctx, recordFixture()); err == nil {
		t.Fatal("ignored cancellation")
	}
}
