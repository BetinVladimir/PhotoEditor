package provenance

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestSigningSchedulerDeploymentOwnsTenantCatalogue(t *testing.T) {
	id := uuid.New()
	d := &SigningSchedulerDeployment{tenants: map[uuid.UUID]SigningSchedulerTenantHandles{
		id: {},
	}}
	first, err := d.TenantHandles()
	if err != nil || len(first) != 1 { t.Fatalf("active catalogue: %v", err) }
	delete(first, id)
	again, err := d.TenantHandles()
	if err != nil || len(again) != 1 { t.Fatal("caller mutated catalogue", err) }
	d.Close()
	if _, err := d.TenantHandles(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("closed deployment leaked handles: %v", err)
	}
	if _, err := InstallSigningSchedulerDeploymentFile(context.Background(),
		"", "", SigningAPIDeploymentRuntime{}); err == nil {
		t.Fatal("unpinned scheduler deployment accepted")
	}
}
