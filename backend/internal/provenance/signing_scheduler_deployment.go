package provenance

import (
	"context"
	"sync/atomic"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2qualification"
)

// SigningSchedulerTenantHandles are installed, live-trust checked dependencies
// for the delegated Delivery signer. The operator remains owned by Deployment.
type SigningSchedulerTenantHandles struct {
	Policies *SigningPolicyRegistry
	NegativePolicies *SigningNegativePolicyRegistry
	Resources *MeasuredSigningResourceQualifier
}

type SigningSchedulerDeployment struct {
	tenants map[uuid.UUID]SigningSchedulerTenantHandles
	services []*InstalledSQLSigningService
	closed atomic.Bool
}

func (d *SigningSchedulerDeployment) TenantHandles() (map[uuid.UUID]SigningSchedulerTenantHandles, error) {
	if d == nil || d.closed.Load() { return nil, ErrInvalid }
	owned := make(map[uuid.UUID]SigningSchedulerTenantHandles, len(d.tenants))
	for id, handles := range d.tenants { owned[id] = handles }
	return owned, nil
}

func (d *SigningSchedulerDeployment) Close() {
	if d == nil || d.closed.Swap(true) { return }
	for _, service := range d.services { service.Close() }
}

// InstallSigningSchedulerDeploymentFile reuses the pinned API document and
// its trusted process handles. No tenant, policy or resource profile is taken
// from a Delivery request. The caller must close the deployment at shutdown.
func InstallSigningSchedulerDeploymentFile(ctx context.Context, path, expectedSHA string,
	runtime SigningAPIDeploymentRuntime) (*SigningSchedulerDeployment, error) {
	if err := ctx.Err(); err != nil { return nil, err }
	raw, err := readPinnedSigningDeployment(ctx, path, expectedSHA)
	if err != nil { return nil, err }
	configs, err := prepareSigningAPIDeployment(ctx, raw, expectedSHA, runtime)
	if err != nil { return nil, err }
	installed := &SigningSchedulerDeployment{
		tenants: make(map[uuid.UUID]SigningSchedulerTenantHandles, len(configs)),
	}
	success := false
	defer func() { if !success { installed.Close() } }()
	for _, config := range configs {
		if err := ctx.Err(); err != nil { return nil, err }
		service, err := NewInstalledSQLSigningServiceContext(ctx, config.Service,
			config.Operator)
		if err != nil { return nil, err }
		installed.services = append(installed.services, service)
		resources, ok := service.Preparation.Resources.(*MeasuredSigningResourceQualifier)
		if !ok || service.Preparation.Registry == nil || resources == nil {
			return nil, ErrInvalid
		}
		negative := &SigningNegativePolicyRegistry{
			policies: map[signingNegativePolicyKey]v2qualification.DeploymentLocks{},
		}
		if config.Service.NegativePolicies != nil {
			negative, err = NewSigningNegativePolicyRegistry(config.Service.NegativePolicies)
			if err != nil { return nil, err }
		}
		id := service.TenantID
		if id == uuid.Nil || installed.tenants[id].Policies != nil {
			return nil, ErrInvalid
		}
		installed.tenants[id] = SigningSchedulerTenantHandles{
			Policies: service.Preparation.Registry, NegativePolicies: negative,
			Resources: resources,
		}
	}
	if err := ctx.Err(); err != nil { return nil, err }
	success = true
	return installed, nil
}
