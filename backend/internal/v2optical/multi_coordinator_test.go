package v2optical

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func TestMultiCoordinatorRejectsDeploymentBoundsBeforeEvidenceIO(t *testing.T) {
	for _, fault := range []string{"pixel_limit", "ram", "pixel_capacity", "operation"} {
		t.Run(fault, func(t *testing.T) {
			operation := "optical.panorama"
			pin, locks, _, _, params, originals := multiQualifiedFixture(t, operation)
			capacity, err := MultiResources(operation, params, originals)
			if err != nil {
				t.Fatal(err)
			}
			reader := &negativeEvidenceIO{}
			coordinator := MultiCoordinator{DB: multiParamsNoIO{}, Reader: reader, Bucket: "photos", Operation: operation, Pin: pin, Locks: locks, MaxPixels: 200000000, Capacity: capacity}
			switch fault {
			case "pixel_limit":
				coordinator.MaxPixels = 1
			case "ram":
				coordinator.Capacity.RAMBytes = 1
			case "pixel_capacity":
				coordinator.Capacity.PixelCount = 0
			case "operation":
				coordinator.Operation = "optical.perspective"
			}
			request := MultiPrepareRequest{Tenant: uuid.New(), Project: uuid.New(), Snapshot: uuid.New(), Key: uuid.NewString(), Params: params, Sources: originals}
			_, err = coordinator.rebuild(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, request)
			if err != v2input.ErrInvalid || reader.reads != 0 {
				t.Fatal("invalid deployment reached IO", err)
			}
		})
	}
}
