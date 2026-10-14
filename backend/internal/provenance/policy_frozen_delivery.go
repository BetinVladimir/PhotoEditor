package provenance

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ResolveFrozenDeliveryPolicy checks the exact installed tenant policy before
// any Delivery export exists. MIME qualification remains a separate per-child
// check after encoding; this method grants no signing or package admission.
func (r *SigningPolicyRegistry) ResolveFrozenDeliveryPolicy(ctx context.Context,
	tenant, policyID uuid.UUID, policySHA string, now time.Time,
) (SigningPolicy, error) {
	var zero SigningPolicy
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if r == nil || tenant == uuid.Nil || policyID == uuid.Nil ||
		!digest(policySHA) || now.IsZero() {
		return zero, ErrInvalid
	}
	projected, err := r.ForTenant(tenant)
	if err != nil {
		return zero, err
	}
	installed, exists := projected.policies[policyID]
	if !exists || installed.ref.SHA256 != policySHA ||
		now.Before(installed.from) || !now.Before(installed.until) {
		return zero, ErrInvalid
	}
	policy, err := DecodeSigningPolicy(installed.body, installed.ref, policyID)
	if err != nil {
		return zero, err
	}
	callback, err := DecodeSigningPolicy(installed.body, installed.ref, policyID)
	if err != nil {
		return zero, err
	}
	if err := projected.authority.CheckSigningOperator(ctx, callback); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return policy, nil
}

func (r *TenantSigningRouter) CheckDeliveryPolicy(ctx context.Context,
	tenant, policyID uuid.UUID, policySHA string) error {
	service, err := r.service(ctx, tenant)
	if err != nil {
		return err
	}
	if service.Preparation.Registry == nil || service.Clock == nil {
		return ErrInvalid
	}
	_, err = service.Preparation.Registry.ResolveFrozenDeliveryPolicy(ctx,
		tenant, policyID, policySHA, service.Clock())
	return err
}

// CheckDeliveryPolicyMIMEs binds the current operator-qualified policy to
// every MIME derived from the server's authorized frozen preset catalogue.
func (r *TenantSigningRouter) CheckDeliveryPolicyMIMEs(ctx context.Context,
	tenant, policyID uuid.UUID, policySHA string, mimes []string) error {
	if !validDeliveryMIMEs(mimes) {
		return ErrInvalid
	}
	service, err := r.service(ctx, tenant)
	if err != nil {
		return err
	}
	if service.Preparation.Registry == nil || service.Clock == nil {
		return ErrInvalid
	}
	policy, err := service.Preparation.Registry.ResolveFrozenDeliveryPolicy(ctx, tenant, policyID, policySHA, service.Clock())
	if err != nil {
		return err
	}
	if !deliveryPolicyAllowsMIMEs(policy, mimes) {
		return ErrInvalid
	}
	return ctx.Err()
}

func deliveryPolicyAllowsMIMEs(policy SigningPolicy, mimes []string) bool {
	if !validDeliveryMIMEs(mimes) {
		return false
	}
	allowed := map[string]bool{}
	for _, mime := range policy.AllowedMIME {
		allowed[mime] = true
	}
	for _, mime := range mimes {
		if !allowed[mime] {
			return false
		}
	}
	return true
}
func validDeliveryMIMEs(mimes []string) bool {
	if len(mimes) < 1 || len(mimes) > 2 {
		return false
	}
	for index, mime := range mimes {
		if mime != "image/jpeg" && mime != "image/png" || index > 0 && mimes[index-1] >= mime {
			return false
		}
	}
	return true
}
