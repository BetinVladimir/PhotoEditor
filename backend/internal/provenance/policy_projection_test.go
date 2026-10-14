package provenance

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestPolicyProjectionSeparatesTenantsAndOwnsDocuments(t *testing.T) {
	config, params, now := signingRegistryFixture(t)
	authority := &signingAuthorityStub{}
	root, err := NewSigningPolicyRegistry(config, authority)
	if err != nil {
		t.Fatal(err)
	}
	tenant, other := uuid.New(), uuid.New()
	first, err := root.ForTenant(tenant)
	if err != nil {
		t.Fatal(err)
	}
	second, err := root.ForTenant(other)
	if err != nil {
		t.Fatal(err)
	}
	again, err := first.ForTenant(tenant)
	if err != nil {
		t.Fatal(err)
	}
	installed := first.policies[params.PolicyID]
	if installed.ref.ArtifactID == second.policies[params.PolicyID].ref.ArtifactID || installed.ref != again.policies[params.PolicyID].ref || installed.ref.SHA256 != params.Policy.SHA256 {
		t.Fatal("unstable or shared tenant identity")
	}
	if _, err := first.ForTenant(other); !errors.Is(err, ErrInvalid) {
		t.Fatal("bound registry rebound", err)
	}
	params.Policy = installed.ref
	reference, body, err := first.PolicyDocument(context.Background(), tenant, params, "image/jpeg", now)
	if err != nil || reference != params.Policy {
		t.Fatal("document rejected", err)
	}
	body[0] = 'x'
	if _, err := first.Resolve(context.Background(), params, "image/jpeg", now); err != nil {
		t.Fatal("returned bytes alias registry", err)
	}
	root.policies[params.PolicyID].body[0] = 'x'
	if _, err := first.Resolve(context.Background(), params, "image/jpeg", now); err != nil {
		t.Fatal("projection aliases template", err)
	}
	if _, _, err := first.PolicyDocument(context.Background(), other, params, "image/jpeg", now); !errors.Is(err, ErrInvalid) {
		t.Fatal("foreign tenant accepted", err)
	}
}

func TestPolicyDocumentRequiresProjectionAndLiveAuthority(t *testing.T) {
	config, params, now := signingRegistryFixture(t)
	authority := &signingAuthorityStub{}
	registry, err := NewSigningPolicyRegistry(config, authority)
	if err != nil {
		t.Fatal(err)
	}
	tenant := uuid.New()
	if _, _, err := registry.PolicyDocument(context.Background(), tenant, params, "image/jpeg", now); !errors.Is(err, ErrInvalid) {
		t.Fatal("template exposed as tenant artifact", err)
	}
	if _, err := registry.ForTenant(uuid.Nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("zero tenant accepted", err)
	}
	registry, err = registry.ForTenant(tenant)
	if err != nil {
		t.Fatal(err)
	}
	params.Policy = registry.policies[params.PolicyID].ref
	authority.err = errors.New("revoked")
	if _, _, err := registry.PolicyDocument(context.Background(), tenant, params, "image/jpeg", now); !errors.Is(err, authority.err) {
		t.Fatal("revoked policy exposed", err)
	}
	authority.err = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := registry.PolicyDocument(ctx, tenant, params, "image/jpeg", now); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel ignored", err)
	}
	if registry.matchesTenant(uuid.New()) || !registry.matchesTenant(tenant) {
		t.Fatal("tenant guard incorrect")
	}
}

func TestSigningCompilationRefusesForeignProjectedRegistryBeforeAuthority(t *testing.T) {
	config, _, now := signingRegistryFixture(t)
	authority := &signingAuthorityStub{}
	registry, err := NewSigningPolicyRegistry(config, authority)
	if err != nil {
		t.Fatal(err)
	}
	registry, err = registry.ForTenant(uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CompileSigningTask(context.Background(), FrozenSigningIntent{tenant: uuid.New()}, registry, "image/jpeg", SigningTaskResources{}, now); !errors.Is(err, ErrInvalid) {
		t.Fatal("foreign compilation accepted", err)
	}
	if authority.calls != 0 {
		t.Fatal("foreign tenant reached live operator")
	}
}
