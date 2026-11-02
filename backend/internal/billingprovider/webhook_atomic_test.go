package billingprovider_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"photoeditor/backend/internal/billingprovider"
)

type stableEventProvider struct {
	billingprovider.Provider
	account string
}

func (p stableEventProvider) ParseEvent(raw []byte, signature, secret string) (billingprovider.Event, error) {
	if signature != "valid" || secret != "test_secret" {
		return billingprovider.Event{}, errors.New("invalid test signature")
	}
	var v struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return billingprovider.Event{}, err
	}
	return billingprovider.Event{ID: v.ID, Type: v.Type, AccountID: p.account}, nil
}
func stableIngress(account string) (*billingprovider.WebhookIngress, *billingprovider.InMemoryWebhookInboxStore) {
	s := billingprovider.NewInMemoryWebhookInboxStore()
	return billingprovider.NewWebhookIngress(stableEventProvider{Provider: billingprovider.UnavailableProvider{}, account: account}, s, "test_secret"), s
}
func TestWebhookAtomicConcurrentDuplicate(t *testing.T) {
	i, s := stableIngress("")
	raw := []byte(`{"id":"evt_same","type":"invoice.paid"}`)
	var wg sync.WaitGroup
	var created atomic.Int64
	for n := 0; n < 32; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e, err := i.IngestRaw(context.Background(), "acct_one", raw, "valid")
			if err != nil {
				t.Error(err)
			}
			if e != nil {
				created.Add(1)
			}
		}()
	}
	wg.Wait()
	if created.Load() != 1 || s.CountInbox() != 1 {
		t.Fatalf("created=%d count=%d", created.Load(), s.CountInbox())
	}
}
func TestWebhookAtomicAccountScopedAndPayloadConflict(t *testing.T) {
	i, s := stableIngress("")
	ctx := context.Background()
	raw := []byte(`{"id":"evt_same","type":"invoice.paid","amount":10}`)
	for _, a := range []string{"acct_one", "acct_two"} {
		if _, err := i.IngestRaw(ctx, a, raw, "valid"); err != nil {
			t.Fatal(err)
		}
	}
	if s.CountInbox() != 2 {
		t.Fatal("event identities must be account scoped")
	}
	if e, err := i.IngestRaw(ctx, "acct_one", raw, "valid"); err != nil || e != nil {
		t.Fatal("exact replay not acknowledged", err)
	}
	_, err := i.IngestRaw(ctx, "acct_one", []byte(`{"id":"evt_same","type":"invoice.paid","amount":11}`), "valid")
	if !errors.Is(err, billingprovider.ErrWebhookPayloadConflict) {
		t.Fatal("changed signed bytes were accepted", err)
	}
}
func TestWebhookAtomicRejectsAccountMismatchAndCancellation(t *testing.T) {
	i, s := stableIngress("acct_other")
	raw := []byte(`{"id":"evt_1","type":"invoice.paid"}`)
	if _, err := i.IngestRaw(context.Background(), "acct_one", raw, "valid"); err == nil {
		t.Fatal("account mismatch accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := i.IngestRaw(ctx, "acct_other", raw, "valid"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if s.CountInbox() != 0 {
		t.Fatal("rejected input was stored")
	}
}
func TestWebhookAtomicCopiesCallerAndReaderBuffers(t *testing.T) {
	i, s := stableIngress("")
	ctx := context.Background()
	raw := []byte(`{"id":"evt_1","type":"invoice.paid"}`)
	entry, err := i.IngestRaw(ctx, "acct_one", raw, "valid")
	if err != nil {
		t.Fatal(err)
	}
	raw[0] = '!'
	entry.RawPayload[0] = '!'
	rows, err := s.ListPending(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].RawPayload[0] != '{' {
		t.Fatal("caller modified stored payload")
	}
	rows[0].RawPayload[0] = '!'
	next, _ := s.ListPending(ctx, 1)
	if next[0].RawPayload[0] != '{' {
		t.Fatal("reader modified stored payload")
	}
	zero, _ := s.ListPending(ctx, 0)
	if len(zero) != 0 {
		t.Fatal("zero limit returned entries")
	}
}
