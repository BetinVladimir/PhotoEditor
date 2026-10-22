package semanticreview

import (
	"github.com/google/uuid"
	"testing"
)

func TestShownApplyInstallationEvidenceDoesNotBorrowTenantOrOperation(t *testing.T) {
	tenant, other := uuid.New(), uuid.New()
	service := &TenantExposureApplyService{services: map[uuid.UUID]*ApplyService{tenant: {}}, installedOperations: map[uuid.UUID]map[string]bool{tenant: {"semantic.adopt_shown_background_cleanup": true}}}
	if !service.SemanticOperationInstalled(tenant, "semantic.adopt_shown_background_cleanup") {
		t.Fatal("own installed operation missing")
	}
	for _, item := range []struct {
		tenant    uuid.UUID
		operation string
	}{{other, "semantic.adopt_shown_background_cleanup"}, {tenant, "semantic.adopt_shown_removal"}, {tenant, "semantic.preview_background_cleanup"}} {
		if service.SemanticOperationInstalled(item.tenant, item.operation) {
			t.Fatal("borrowed installation")
		}
	}
	var missing *TenantExposureApplyService
	if missing.SemanticOperationInstalled(tenant, "semantic.adopt_shown_background_cleanup") {
		t.Fatal("nil installation")
	}
}

func TestShownRetouchInstallationRequiresOwnTenantService(t *testing.T) {
	const operation = "semantic.adopt_shown_retouch_region"
	tenant, other := uuid.New(), uuid.New()
	service := &TenantExposureApplyService{
		services: map[uuid.UUID]*ApplyService{tenant: {}, other: {}},
		installedOperations: map[uuid.UUID]map[string]bool{tenant: {operation: true}, other: {"semantic.adopt_shown_removal": true}},
	}
	if !service.SemanticOperationInstalled(tenant, operation) { t.Fatal("own retouch installation missing") }
	for _, item := range []struct { tenant uuid.UUID; operation string }{
		{other, operation}, {uuid.New(), operation}, {uuid.Nil, operation},
		{tenant, "semantic.preview_retouch_region"}, {tenant, "semantic.adopt_shown_removal"},
	} {
		if service.SemanticOperationInstalled(item.tenant, item.operation) { t.Fatal("borrowed tenant/operation installation") }
	}
	service.services[tenant] = nil
	if service.SemanticOperationInstalled(tenant, operation) { t.Fatal("missing service advertised installed") }
}
