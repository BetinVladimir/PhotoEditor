package provenance

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestFrozenDeliveryPolicyChecksExactTenantAndLiveOperator(t *testing.T) {
	config, params, now := signingRegistryFixture(t)
	authority := &signingAuthorityStub{}
	registry, err := NewSigningPolicyRegistry(config, authority)
	if err != nil {
		t.Fatal(err)
	}
	tenant := uuid.New()
	policy, err := registry.ResolveFrozenDeliveryPolicy(context.Background(),
		tenant, params.PolicyID, params.Policy.SHA256, now)
	if err != nil || policy.ID != params.PolicyID || authority.calls != 1 {
		t.Fatal("installed policy not checked", err)
	}
	if _, err := registry.ResolveFrozenDeliveryPolicy(context.Background(), tenant,
		params.PolicyID, strings.Repeat("b", 64), now); err == nil {
		t.Fatal("substituted policy digest accepted")
	}
	cause := errors.New("operator revoked")
	authority.err = cause
	if _, err := registry.ResolveFrozenDeliveryPolicy(context.Background(), tenant,
		params.PolicyID, params.Policy.SHA256, now); !errors.Is(err, cause) {
		t.Fatal("live operator refusal ignored", err)
	}
}
func TestFrozenDeliveryPolicyCoversEverySelectedMIME(t *testing.T) {
	policy := SigningPolicy{AllowedMIME: []string{"image/jpeg"}}
	if !deliveryPolicyAllowsMIMEs(policy, []string{"image/jpeg"}) {
		t.Fatal("JPEG refused")
	}
	for _, mimes := range [][]string{{"image/png"}, {"image/jpeg", "image/png"}, {"image/jpeg", "image/jpeg"}, {"image/tiff"}, {}} {
		if deliveryPolicyAllowsMIMEs(policy, mimes) {
			t.Fatal("unqualified MIME set accepted", mimes)
		}
	}
	policy.AllowedMIME = []string{"image/jpeg", "image/png"}
	if !deliveryPolicyAllowsMIMEs(policy, []string{"image/jpeg", "image/png"}) {
		t.Fatal("qualified pair refused")
	}
}
