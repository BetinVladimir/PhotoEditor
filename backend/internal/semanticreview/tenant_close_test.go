package semanticreview

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
)

func TestTenantCloseSelectsOnlyOwnInstallation(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	aService, bService, replayOnly := &CloseService{}, &CloseService{}, &CloseService{}
	service := &TenantExposureApplyService{
		closeServices: map[uuid.UUID]*CloseService{a: aService, b: bService},
		closeFallback: replayOnly,
	}
	if service.closeServiceForTenant(a) != aService || service.closeServiceForTenant(b) != bService {
		t.Fatal("configured tenant must retain its own preparation")
	}
	if service.closeServiceForTenant(uuid.New()) != replayOnly || replayOnly.Prepare != nil {
		t.Fatal("unknown tenant inherited another organization's preparation")
	}
	var unavailable *TenantExposureApplyService
	if _, err := unavailable.Close(context.Background(), auth.Identity{}, a, uuid.New(), "key", ReviewClose{}); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}
