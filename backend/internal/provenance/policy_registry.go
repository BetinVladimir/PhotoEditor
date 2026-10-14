package provenance

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"time"
)

// Authority must check live key availability and qualified certificate chain,
// expiry/revocation and timestamp service policy. Registry pins alone do not
// establish cryptographic trust or grant permission to sign an export.
type SigningOperatorAuthority interface {
	CheckSigningOperator(context.Context, SigningPolicy) error
}
type InstalledSigningPolicy struct {
	Ref       v2input.Ref
	Body      []byte
	NotBefore time.Time
	NotAfter  time.Time
}

// MaxInstalledSigningPolicies matches the isolated signer and public catalogue bounds.
const MaxInstalledSigningPolicies = 256

type SigningInstallation struct {
	SDKLockSHA          string
	CertificateChainSHA string
	TrustBundleSHA      string
	KeyHandles          []string
	TimestampServices   []string
	Policies            []InstalledSigningPolicy
}
type installedSigningPolicy struct {
	ref         v2input.Ref
	body        []byte
	from, until time.Time
}
type SigningPolicyRegistry struct {
	tenant    uuid.UUID
	policies  map[uuid.UUID]installedSigningPolicy
	authority SigningOperatorAuthority
}

func NewSigningPolicyRegistry(config SigningInstallation, authority SigningOperatorAuthority) (*SigningPolicyRegistry, error) {
	if authority == nil || !digest(config.SDKLockSHA) || !digest(config.CertificateChainSHA) || !digest(config.TrustBundleSHA) || len(config.Policies) < 1 || len(config.Policies) > MaxInstalledSigningPolicies || len(config.KeyHandles) < 1 || len(config.KeyHandles) > 1024 || len(config.TimestampServices) > 1024 {
		return nil, ErrInvalid
	}
	keys := map[string]bool{}
	services := map[string]bool{}
	for _, key := range config.KeyHandles {
		if !text(key, 200) || keys[key] {
			return nil, ErrInvalid
		}
		keys[key] = true
	}
	for _, service := range config.TimestampServices {
		if !text(service, 120) || services[service] {
			return nil, ErrInvalid
		}
		services[service] = true
	}
	registry := &SigningPolicyRegistry{policies: map[uuid.UUID]installedSigningPolicy{}, authority: authority}
	total := 0
	for _, installed := range config.Policies {
		body := append([]byte(nil), installed.Body...)
		total += len(body)
		if total > 8<<20 || installed.NotBefore.IsZero() || !installed.NotAfter.After(installed.NotBefore) {
			return nil, ErrInvalid
		}
		var header SigningPolicy
		if decode(body, &header, 1<<20) != nil {
			return nil, ErrInvalid
		}
		policy, err := DecodeSigningPolicy(body, installed.Ref, header.ID)
		if err != nil {
			return nil, err
		}
		if _, exists := registry.policies[policy.ID]; exists {
			return nil, ErrInvalid
		}
		if policy.SDKLockSHA != config.SDKLockSHA || policy.CertificateChainSHA != config.CertificateChainSHA || policy.TrustBundleSHA != config.TrustBundleSHA || !keys[policy.KeyHandle] {
			return nil, ErrInvalid
		}
		if policy.TimestampServiceID != nil && !services[*policy.TimestampServiceID] {
			return nil, ErrInvalid
		}
		registry.policies[policy.ID] = installedSigningPolicy{ref: installed.Ref, body: body, from: installed.NotBefore, until: installed.NotAfter}
	}
	return registry, nil
}

// Resolve is invocation-local and rechecks live operator availability. The
// immutable registry must be replaced when operator config/policy is rotated.
// now comes from the server clock, never from request or archived metadata.
func (r *SigningPolicyRegistry) Resolve(ctx context.Context, params SignParams, mime string, now time.Time) (SigningPolicy, error) {
	var zero SigningPolicy
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if r == nil || r.authority == nil || !validSignParams(params) || now.IsZero() {
		return zero, ErrInvalid
	}
	installed, exists := r.policies[params.PolicyID]
	if !exists || installed.ref != params.Policy || now.Before(installed.from) || !now.Before(installed.until) {
		return zero, ErrInvalid
	}
	policy, err := DecodeSigningPolicy(installed.body, installed.ref, params.PolicyID)
	if err != nil {
		return zero, err
	}
	allowed := false
	for _, candidate := range policy.AllowedMIME {
		if candidate == mime {
			allowed = true
		}
	}
	if !allowed {
		return zero, ErrInvalid
	}
	// Decode a separate value for callbacks, retaining no shared slices/pointers.
	callback, err := DecodeSigningPolicy(installed.body, installed.ref, params.PolicyID)
	if err != nil {
		return zero, err
	}
	if err := r.authority.CheckSigningOperator(ctx, callback); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return policy, nil
}

// ResolveAdoptedPolicy rechecks a frozen tenant policy and live operator for a
// previously adopted signed export. The caller separately proves its exact
// export/lineage/task ancestry; no synthetic SignParams are constructed.
func (r *SigningPolicyRegistry) ResolveAdoptedPolicy(ctx context.Context,
	tenant, policyID uuid.UUID, policySHA, mime string, now time.Time,
) (SigningPolicy, error) {
	var zero SigningPolicy
	if err := ctx.Err(); err != nil { return zero, err }
	if r == nil || tenant == uuid.Nil || policyID == uuid.Nil ||
		!digest(policySHA) || (mime != "image/jpeg" && mime != "image/png") {
		return zero, ErrInvalid
	}
	projected, err := r.ForTenant(tenant)
	if err != nil { return zero, err }
	installed, exists := projected.policies[policyID]
	if !exists || installed.ref.SHA256 != policySHA || now.IsZero() ||
		now.Before(installed.from) || !now.Before(installed.until) {
		return zero, ErrInvalid
	}
	policy, err := DecodeSigningPolicy(installed.body, installed.ref, policyID)
	if err != nil { return zero, err }
	allowed := false
	for _, candidate := range policy.AllowedMIME {
		if candidate == mime { allowed = true }
	}
	if !allowed { return zero, ErrInvalid }
	callback, err := DecodeSigningPolicy(installed.body, installed.ref, policyID)
	if err != nil { return zero, err }
	if err := projected.authority.CheckSigningOperator(ctx, callback); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil { return zero, err }
	return policy, nil
}
