package provenance

import (
	"context"
	"sync/atomic"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Immutable process-installed tenant catalogue. Request bodies cannot select
// operators, workers or fallback tenants. Authentication/authorization still
// executes inside the selected SQL service; routing itself grants no access.
type TenantSigningRouter struct {
	services map[uuid.UUID]*InstalledSQLSigningService
	closed   atomic.Bool
}

// Successful construction transfers lifecycle ownership to the router. On
// refusal ownership remains with caller; no supplied services are closed.
func NewTenantSigningRouter(installed []*InstalledSQLSigningService) (*TenantSigningRouter, error) {
	if len(installed) < 1 || len(installed) > 100000 {
		return nil, ErrInvalid
	}
	services := make(map[uuid.UUID]*InstalledSQLSigningService, len(installed))
	operators := map[*InstalledChallengeSigningAuthority]bool{}
	for _, service := range installed {
		if service == nil || service.TenantID == uuid.Nil || service.SQLSignExportService == nil || service.Operator == nil || service.Operator.closed.Load() || service.Preparation.Registry == nil || service.Preparation.Registry.tenant != service.TenantID {
			return nil, ErrInvalid
		}
		if services[service.TenantID] != nil || operators[service.Operator] {
			return nil, ErrInvalid
		}
		// Freeze both public wrapper and SQL service fields; keep the exact installed
		// authority and its independently owned registry/source implementations.
		owned := *service
		sql := *service.SQLSignExportService
		owned.SQLSignExportService = &sql
		services[owned.TenantID] = &owned
		operators[owned.Operator] = true
	}
	return &TenantSigningRouter{services: services}, nil
}
func (r *TenantSigningRouter) Close() {
	if r == nil || r.closed.Swap(true) {
		return
	}
	for _, service := range r.services {
		service.Close()
	}
}
func (r *TenantSigningRouter) service(ctx context.Context, tenant uuid.UUID) (*InstalledSQLSigningService, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.closed.Load() || tenant == uuid.Nil {
		return nil, ErrInvalid
	}
	service := r.services[tenant]
	if service == nil || service.Operator.closed.Load() {
		return nil, ErrInvalid
	}
	return service, nil
}
func (r *TenantSigningRouter) SignExport(ctx context.Context, who auth.Identity, tenant, export, policy uuid.UUID, key string) (v2input.JobTicket, error) {
	service, err := r.service(ctx, tenant)
	if err != nil {
		return v2input.JobTicket{}, err
	}
	return service.SignExport(ctx, who, tenant, export, policy, key)
}
func (r *TenantSigningRouter) AvailableSigningPolicies(ctx context.Context, who auth.Identity, tenant, export uuid.UUID) ([]SigningPolicyChoice, error) {
	service, err := r.service(ctx, tenant)
	if err != nil {
		return nil, err
	}
	return service.AvailableSigningPolicies(ctx, who, tenant, export)
}
func (r *TenantSigningRouter) AvailableDeliverySigningPolicies(ctx context.Context, who auth.Identity, tenant, project uuid.UUID, mimes []string) ([]DeliverySigningPolicyChoice, error) {
	service, err := r.service(ctx, tenant)
	if err != nil {
		return nil, err
	}
	return service.AvailableDeliverySigningPolicies(ctx, who, tenant, project, mimes)
}
