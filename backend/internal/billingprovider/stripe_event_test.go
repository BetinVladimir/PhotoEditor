package billingprovider

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	stripe "github.com/stripe/stripe-go/v87"
)

func TestStripeSignedSnapshot(t *testing.T) {
	p, err := NewStripeEventParser("acct_sandbox", "2026-10-28.endive")
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"id":"evt_test","object":"event","type":"invoice.paid","api_version":"2026-10-28.endive","livemode":false,"created":1793491200,"data":{"object":{"id":"in_test"}}}`)
	secret := "test-fixture-key"
	sign := func(body []byte, when time.Time) string {
		return stripe.GenerateTestSignedPayload(&stripe.UnsignedPayload{Payload: body, Secret: secret, Timestamp: when}).Header
	}
	header := sign(payload, time.Now())
	event, err := p.ParseEvent(payload, header, secret)
	if err != nil || event.ID != "evt_test" || event.AccountID != "acct_sandbox" || !bytes.Equal(event.RawPayload, payload) {
		t.Fatalf("parse: %+v %v", event, err)
	}
	event.RawPayload[0] = '!'
	if payload[0] != '{' {
		t.Fatal("parser retained caller buffer")
	}
	cases := []struct {
		name           string
		body           []byte
		header, secret string
	}{
		{"tampered", append(append([]byte(nil), payload...), ' '), header, secret},
		{"wrong_secret", payload, header, "wrong-fixture-key"},
		{"empty_secret", payload, header, ""},
		{"missing_signature", payload, "", secret},
		{"expired", payload, sign(payload, time.Now().Add(-10*time.Minute)), secret},
		{"future", payload, sign(payload, time.Now().Add(10*time.Minute)), secret},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := p.ParseEvent(tc.body, tc.header, tc.secret); err == nil {
				t.Fatal("accepted invalid signature")
			}
		})
	}
	for _, tc := range []struct{ name, old, new string }{
		{"live", `"livemode":false`, `"livemode":true`},
		{"missing_mode", `"livemode":false,`, ``},
		{"wrong_version", `2026-10-28.endive`, `2026-09-30.clover`},
		{"wrong_account", `"object":"event"`, `"object":"event","account":"acct_other"`},
		{"thin_event", `"object":"event"`, `"object":"event_notification"`},
		{"missing_data", `"data":{"object":{"id":"in_test"}}`, `"data":{}`},
		{"missing_object_id", `"id":"in_test"`, `"object":"invoice"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(strings.Replace(string(payload), tc.old, tc.new, 1))
			if _, err := p.ParseEvent(body, sign(body, time.Now()), secret); err == nil {
				t.Fatal("accepted invalid envelope")
			}
		})
	}
	t.Run("malformed_signed_json", func(t *testing.T) {
		body := []byte(`{"id":`)
		if _, err := p.ParseEvent(body, sign(body, time.Now()), secret); err == nil {
			t.Fatal("accepted malformed JSON")
		}
	})
}

type failingStripeInbox struct{ WebhookInboxStore }

func (failingStripeInbox) Record(context.Context, WebhookInboxEntry) (bool, error) {
	return false, errors.New("test commit failed")
}

func TestStripeSignatureIngressRequiresInboxCommit(t *testing.T) {
	parser, err := NewStripeEventParser("acct_sandbox", "2026-10-28.endive")
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"id":"evt_commit","object":"event","type":"invoice.paid","api_version":"2026-10-28.endive","livemode":false,"created":1793491200,"data":{"object":{"id":"in_test"}}}`)
	secret := "test-fixture-key"
	header := stripe.GenerateTestSignedPayload(&stripe.UnsignedPayload{Payload: body, Secret: secret}).Header
	store := NewInMemoryWebhookInboxStore()
	ingress := NewWebhookIngress(parser, store, secret)
	if _, err := ingress.IngestRaw(context.Background(), "acct_sandbox", body, header+"0"); err == nil || store.CountInbox() != 0 {
		t.Fatal("invalid signature persisted")
	}
	if entry, err := ingress.IngestRaw(context.Background(), "acct_sandbox", body, header); err != nil || entry == nil || store.CountInbox() != 1 {
		t.Fatal("valid event not committed", err)
	}
	if entry, err := ingress.IngestRaw(context.Background(), "acct_sandbox", body, header); err != nil || entry != nil || store.CountInbox() != 1 {
		t.Fatal("signed duplicate not idempotent", err)
	}
	failing := NewWebhookIngress(parser, failingStripeInbox{}, secret)
	if entry, err := failing.IngestRaw(context.Background(), "acct_sandbox", body, header); err == nil || entry != nil {
		t.Fatal("acknowledged failed commit")
	}
}
