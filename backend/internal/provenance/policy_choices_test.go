package provenance

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSigningPolicyChoicesAreRedactedAndLive(t *testing.T) {
	for _, mode := range []string{"available", "expired", "not_yet_valid", "denied", "cancelled", "mutation"} {
		t.Run(mode, func(t *testing.T) {
			config, params, now := signingRegistryFixture(t)
			authority := &signingAuthorityStub{}
			if mode == "denied" {
				authority.err = errors.New("key offline")
			}
			if mode == "mutation" {
				authority.mutate = true
			}
			registry, err := NewSigningPolicyRegistry(config, authority)
			if err != nil {
				t.Fatal(err)
			}
			when := now
			if mode == "expired" {
				when = config.Policies[0].NotAfter
			}
			if mode == "not_yet_valid" {
				when = config.Policies[0].NotBefore.Add(-time.Second)
			}
			ctx := context.Background()
			if mode == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				authority.cancel = cancel
			}
			choices, err := registry.available(ctx, "image/jpeg", when)
			if mode == "cancelled" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			if mode == "available" || mode == "mutation" {
				count = 1
			}
			if len(choices) != count {
				t.Fatalf("choices %d want %d", len(choices), count)
			}
			if count == 1 {
				if choices[0].ID != params.PolicyID || choices[0].Revision != "1" {
					t.Fatal("policy identity changed")
				}
				raw, e := json.Marshal(choices)
				if e != nil {
					t.Fatal(e)
				}
				if strings.Contains(string(raw), "key") || strings.Contains(string(raw), "sdk") || strings.Contains(string(raw), "trust") {
					t.Fatal("operator configuration leaked")
				}
				choices[0].Revision = "mutated"
				again, e := registry.available(context.Background(), "image/jpeg", now)
				if e != nil || len(again) != 1 || again[0].Revision != "1" {
					t.Fatal("registry changed", e)
				}
			}
		})
	}
}

func TestDeliverySigningChoicesPinPolicyAndEveryMIME(t *testing.T) {
	config, params, now := signingRegistryFixture(t)
	authority := &signingAuthorityStub{mutate: true}
	registry, err := NewSigningPolicyRegistry(config, authority)
	if err != nil {
		t.Fatal(err)
	}
	choices, err := registry.availableDelivery(context.Background(), []string{"image/jpeg", "image/png"}, now)
	if err != nil || len(choices) != 1 || choices[0].ID != params.PolicyID || choices[0].SHA256 != params.Policy.SHA256 || choices[0].Revision != "1" {
		t.Fatal("delivery choice missing exact policy", choices, err)
	}
	if authority.calls != 1 {
		t.Fatal("operator not checked")
	}
	raw, err := json.Marshal(choices)
	if err != nil || strings.Contains(string(raw), "operator-key") || strings.Contains(string(raw), "operator-tsa") {
		t.Fatal("operator secret leaked", err)
	}
	for _, mimes := range [][]string{{}, {"image/tiff"}, {"image/jpeg", "image/jpeg"}, {"image/jpeg", "image/png", "image/tiff"}} {
		if _, err := registry.availableDelivery(context.Background(), mimes, now); err == nil {
			t.Fatal("invalid MIME selection accepted", mimes)
		}
	}
	authority.err = errors.New("key offline")
	choices, err = registry.availableDelivery(context.Background(), []string{"image/jpeg"}, now)
	if err != nil || len(choices) != 0 {
		t.Fatal("unqualified policy exposed", choices, err)
	}
	choices, err = registry.availableDelivery(context.Background(), []string{"image/jpeg"}, config.Policies[0].NotAfter)
	if err != nil || len(choices) != 0 {
		t.Fatal("expired policy exposed", choices, err)
	}
}

func TestDeliverySigningChoicesRecheckOperatorBeforeResponse(t *testing.T) {
	config, _, now := signingRegistryFixture(t)
	authority := &signingAuthorityStub{}
	registry, err := NewSigningPolicyRegistry(config, authority)
	if err != nil {
		t.Fatal(err)
	}
	choices, err := registry.availableDelivery(context.Background(), []string{"image/jpeg"}, now)
	if err != nil || len(choices) != 1 || authority.calls != 1 {
		t.Fatal("initial policy qualification failed", err)
	}
	authority.err = errors.New("operator key revoked during project authorization")
	current, err := currentDeliverySigningChoices(context.Background(), registry, choices, now)
	if err != nil || len(current) != 0 || authority.calls != 2 {
		t.Fatal("revoked policy exposed", current, err)
	}
	authority.err = nil
	current, err = currentDeliverySigningChoices(context.Background(), registry, choices, now)
	if err != nil || len(current) != 1 || authority.calls != 3 {
		t.Fatal("restored qualified policy missing", current, err)
	}
	current, err = currentDeliverySigningChoices(context.Background(), registry, choices, config.Policies[0].NotAfter)
	if err != nil || len(current) != 0 || authority.calls != 3 {
		t.Fatal("expired policy exposed or operator called", current, err)
	}
}
