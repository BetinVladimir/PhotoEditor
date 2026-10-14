package provenance

import (
	"context"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// ForTenant derives storage identities only. It neither publishes artifacts nor
// proves their presence, ownership, quota or current project access. Admission
// must still verify physical tenant-local policy artifacts through SQL/storage.
// Rotation changes content SHA and thus artifact identity; equal content has a
// stable identity across retries in the same tenant.
func (r *SigningPolicyRegistry) ForTenant(tenant uuid.UUID) (*SigningPolicyRegistry, error) {
	if r == nil || r.authority == nil || tenant == uuid.Nil || (r.tenant != uuid.Nil && r.tenant != tenant) {
		return nil, ErrInvalid
	}
	projected := &SigningPolicyRegistry{tenant: tenant, authority: r.authority, policies: make(map[uuid.UUID]installedSigningPolicy, len(r.policies))}
	for id, installed := range r.policies {
		if _, err := DecodeSigningPolicy(installed.body, installed.ref, id); err != nil {
			return nil, err
		}
		projected.policies[id] = installedSigningPolicy{
			ref:  v2input.Ref{ArtifactID: uuid.NewSHA1(tenant, []byte("signing:policy:"+id.String()+":"+installed.ref.SHA256)), SHA256: installed.ref.SHA256},
			body: append([]byte(nil), installed.body...), from: installed.from, until: installed.until,
		}
	}
	return projected, nil
}

func (r *SigningPolicyRegistry) matchesTenant(tenant uuid.UUID) bool {
	return r != nil && tenant != uuid.Nil && (r.tenant == uuid.Nil || r.tenant == tenant)
}

// PolicyDocument returns owned installed bytes only after current policy/MIME
// and operator checks. It is intentionally restricted to a tenant projection.
// The returned ref is an expected storage identity, never a ready receipt.
func (r *SigningPolicyRegistry) PolicyDocument(ctx context.Context, tenant uuid.UUID, params SignParams, mime string, now time.Time) (v2input.Ref, []byte, error) {
	if r == nil || r.tenant == uuid.Nil || r.tenant != tenant {
		return v2input.Ref{}, nil, ErrInvalid
	}
	if _, err := r.Resolve(ctx, params, mime, now); err != nil {
		return v2input.Ref{}, nil, err
	}
	installed := r.policies[params.PolicyID]
	return installed.ref, append([]byte(nil), installed.body...), nil
}
