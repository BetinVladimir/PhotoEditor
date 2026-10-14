package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

func TestInstallDeliverySigningStageRejectsPartialTrust(t *testing.T) {
	job, err := InstallDeliverySigningStage(DeliveryExpansionJob{}, DeliverySigningRuntime{})
	if !errors.Is(err, v2input.ErrInvalid) || job.Driver.Signing != nil ||
		job.Driver.SignedAssembly != nil {
		t.Fatalf("partial signer runtime was installed: %v", err)
	}
}

func TestTenantSigningRouterRejectsUnknownTenant(t *testing.T) {
	router := tenantDeliverySigningProcessor{runtime: DeliverySigningRuntime{
		Tenants: map[uuid.UUID]DeliverySigningTenantRuntime{},
	}, pageSize: 10}
	state, err := router.ProcessSigning(context.Background(),
		delivery.ExpansionLease{Tenant: uuid.New()}, delivery.PreparePlan{})
	if state != "" || !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("unknown tenant reached signing stage: %q %v", state, err)
	}
}

func TestDeliverySigningRuntimeRequiresInstalledDeployment(t *testing.T) {
	_, err := DeliverySigningRuntimeFromDeployment(nil, nil, nil, nil,
		"photos", "", nil, nil)
	if !errors.Is(err, v2input.ErrInvalid) {
		t.Fatalf("missing pinned deployment accepted: %v", err)
	}
}
