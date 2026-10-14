package provenance

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
	"time"
)

type signingAuthorityStub struct {
	calls  int
	err    error
	mutate bool
	cancel context.CancelFunc
}

func (s *signingAuthorityStub) CheckSigningOperator(_ context.Context, p SigningPolicy) error {
	s.calls++
	if s.mutate {
		p.AllowedMIME[0] = "changed"
		if p.TimestampServiceID != nil {
			*p.TimestampServiceID = "changed"
		}
	}
	if s.cancel != nil {
		s.cancel()
	}
	return s.err
}
func signingRegistryFixture(t *testing.T) (SigningInstallation, SignParams, time.Time) {
	t.Helper()
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	service := "operator-tsa"
	p := SigningPolicy{Schema: 1, ID: uuid.New(), Revision: "1", SDKLockSHA: sha, KeyHandle: "operator-key", CertificateChainSHA: sha, TimestampMode: "required", TimestampServiceID: &service, AllowedMIME: []string{"image/jpeg", "image/png"}, TrustBundleSHA: sha, RevocationMode: "required"}
	raw, reference := contractBytes(t, p)
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	config := SigningInstallation{SDKLockSHA: sha, CertificateChainSHA: sha, TrustBundleSHA: sha, KeyHandles: []string{"operator-key"}, TimestampServices: []string{service}, Policies: []InstalledSigningPolicy{{Ref: reference, Body: raw, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}}}
	params := SignParams{Schema: 1, PolicyID: p.ID, Policy: reference, Export: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, Lineage: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}}
	return config, params, now
}
func TestSigningRegistryOwnsConfigurationAndChecksLiveAuthority(t *testing.T) {
	config, params, now := signingRegistryFixture(t)
	authority := &signingAuthorityStub{mutate: true}
	registry, err := NewSigningPolicyRegistry(config, authority)
	if err != nil {
		t.Fatal(err)
	}
	config.Policies[0].Body[0] = 'x'
	config.KeyHandles[0] = "changed"
	p, err := registry.Resolve(context.Background(), params, "image/jpeg", now)
	if err != nil || p.AllowedMIME[0] != "image/jpeg" || *p.TimestampServiceID != "operator-tsa" {
		t.Fatal("mutated policy", err)
	}
	p.AllowedMIME[0] = "changed"
	*p.TimestampServiceID = "changed"
	if _, err := registry.Resolve(context.Background(), params, "image/jpeg", now); err != nil || authority.calls != 2 {
		t.Fatal("registry aliased result or skipped live authority", err)
	}
	cause := errors.New("key unavailable")
	authority.err = cause
	if _, err := registry.Resolve(context.Background(), params, "image/jpeg", now); !errors.Is(err, cause) {
		t.Fatal("authority failure lost", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	authority.err = nil
	authority.cancel = cancel
	if _, err := registry.Resolve(ctx, params, "image/jpeg", now); err != context.Canceled {
		t.Fatal("cancelled authority accepted", err)
	}
}
func TestSigningRegistryRejectsForeignExpiredAndUninstalledPolicies(t *testing.T) {
	for _, mode := range []string{"id", "ref", "sha", "mime", "before", "expired", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			config, params, now := signingRegistryFixture(t)
			authority := &signingAuthorityStub{}
			registry, err := NewSigningPolicyRegistry(config, authority)
			if err != nil {
				t.Fatal(err)
			}
			mime := "image/jpeg"
			ctx := context.Background()
			switch mode {
			case "id":
				params.PolicyID = uuid.New()
			case "ref":
				params.Policy.ArtifactID = uuid.New()
			case "sha":
				params.Policy.SHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			case "mime":
				mime = "image/tiff"
			case "before":
				now = config.Policies[0].NotBefore.Add(-time.Nanosecond)
			case "expired":
				now = config.Policies[0].NotAfter
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if _, err := registry.Resolve(ctx, params, mime, now); err == nil || authority.calls != 0 {
				t.Fatal("invalid scope reached authority", err)
			}
		})
	}
	for _, mode := range []string{"keys", "tsa", "sdk", "trust", "certificate", "duplicate", "window", "authority"} {
		config, _, _ := signingRegistryFixture(t)
		var authority SigningOperatorAuthority = &signingAuthorityStub{}
		switch mode {
		case "keys":
			config.KeyHandles = []string{"unknown"}
		case "tsa":
			config.TimestampServices = nil
		case "sdk":
			config.SDKLockSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		case "trust":
			config.TrustBundleSHA = "invalid"
		case "certificate":
			config.CertificateChainSHA = "invalid"
		case "duplicate":
			config.Policies = append(config.Policies, config.Policies[0])
		case "window":
			config.Policies[0].NotAfter = config.Policies[0].NotBefore
		case "authority":
			authority = nil
		}
		if _, err := NewSigningPolicyRegistry(config, authority); err == nil {
			t.Fatal("invalid installation accepted", mode)
		}
	}
}

func TestSigningRegistryPolicyCapacityMatchesWorkerAndCatalogue(t *testing.T) {
	config, _, _ := signingRegistryFixture(t)
	template := config.Policies[0]
	config.Policies = nil
	for i := 0; i < MaxInstalledSigningPolicies; i++ {
		var policy SigningPolicy
		if err := decode(template.Body, &policy, 1<<20); err != nil {
			t.Fatal(err)
		}
		policy.ID = uuid.New()
		body, reference := contractBytes(t, policy)
		installed := template
		installed.Body, installed.Ref = body, reference
		config.Policies = append(config.Policies, installed)
	}
	authority := &signingAuthorityStub{}
	if _, err := NewSigningPolicyRegistry(config, authority); err != nil {
		t.Fatal("maximum supported installation rejected", err)
	}
	config.Policies = append(config.Policies, template)
	if _, err := NewSigningPolicyRegistry(config, authority); !errors.Is(err, ErrInvalid) {
		t.Fatal("oversized installation accepted", err)
	}
	if authority.calls != 0 {
		t.Fatal("installation unexpectedly invoked live operator")
	}
}

func TestSigningRegistryRefusesAliasedInputsBeforeNativeAuthority(t *testing.T) {
	for _, pair := range []string{"export_lineage", "export_policy", "lineage_policy"} {
		t.Run(pair, func(t *testing.T) {
			config, params, now := signingRegistryFixture(t)
			authority := &signingAuthorityStub{}
			registry, err := NewSigningPolicyRegistry(config, authority)
			if err != nil {
				t.Fatal(err)
			}
			switch pair {
			case "export_lineage":
				params.Export.ArtifactID = params.Lineage.ArtifactID
			case "export_policy":
				params.Export.ArtifactID = params.Policy.ArtifactID
			case "lineage_policy":
				params.Lineage.ArtifactID = params.Policy.ArtifactID
			}
			if _, err := registry.Resolve(context.Background(), params, "image/jpeg", now); !errors.Is(err, ErrInvalid) {
				t.Fatal("typed alias accepted", err)
			}
			if authority.calls != 0 {
				t.Fatal("aliased inputs reached native authority")
			}
		})
	}
}
