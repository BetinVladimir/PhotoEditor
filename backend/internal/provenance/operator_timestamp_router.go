package provenance

import (
	"bytes"
	"context"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// SigningTimestampRouter binds installed policies to process-owned TSA checks.
// Each check must verify its actual qualified timestamp service; this router
// supplies neither RFC3161 verification nor an availability-based approval.
type SigningTimestampRouter struct {
	policies map[string][]byte
	services map[string]SigningTrustCheck
}

func NewSigningTimestampRouter(policies [][]byte, services map[string]SigningTrustCheck) (*SigningTimestampRouter, error) {
	if len(policies) < 1 || len(policies) > MaxInstalledSigningPolicies || len(services) < 1 || len(services) > MaxInstalledSigningPolicies {
		return nil, ErrInvalid
	}
	router := &SigningTimestampRouter{policies: map[string][]byte{}, services: map[string]SigningTrustCheck{}}
	ids := map[string]bool{}
	used := map[string]bool{}
	total := 0
	for id, check := range services {
		if !text(id, 120) || check == nil {
			return nil, ErrInvalid
		}
		router.services[id] = check
	}
	for _, raw := range policies {
		if len(raw) < 2 || len(raw) > 1<<20 || len(raw) > (8<<20)-total {
			return nil, ErrInvalid
		}
		total += len(raw)
		var header SigningPolicy
		if decode(raw, &header, 1<<20) != nil {
			return nil, ErrInvalid
		}
		pin := bodyDigest(raw)
		policy, err := DecodeSigningPolicy(raw, v2input.Ref{ArtifactID: header.ID, SHA256: pin}, header.ID)
		if err != nil {
			return nil, err
		}
		if ids[policy.ID.String()] || policy.TimestampMode != "required" || policy.TimestampServiceID == nil {
			return nil, ErrInvalid
		}
		id := *policy.TimestampServiceID
		if router.services[id] == nil {
			return nil, ErrInvalid
		}
		ids[policy.ID.String()] = true
		used[id] = true
		router.policies[pin] = append([]byte(nil), raw...)
	}
	// Unused declarations usually indicate a mismatched installation. Refuse
	// rather than silently keeping an unintended fallback authority.
	if len(used) != len(router.services) {
		return nil, ErrInvalid
	}
	return router, nil
}

func (r *SigningTimestampRouter) Check(ctx context.Context, policy SigningPolicy, chain []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || len(chain) < 1 || len(chain) > 1<<20 {
		return ErrInvalid
	}
	raw, err := canonical.Marshal(policy)
	if err != nil {
		return err
	}
	expected, ok := r.policies[bodyDigest(raw)]
	if !ok || !bytes.Equal(raw, expected) || bodyDigest(chain) != policy.CertificateChainSHA || policy.TimestampServiceID == nil {
		return ErrInvalid
	}
	check := r.services[*policy.TimestampServiceID]
	if check == nil {
		return ErrInvalid
	}
	owned, err := DecodeSigningPolicy(raw, v2input.Ref{ArtifactID: policy.ID, SHA256: bodyDigest(raw)}, policy.ID)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = check(ctx, owned, append([]byte(nil), chain...)); err != nil {
		return err
	}
	return ctx.Err()
}
