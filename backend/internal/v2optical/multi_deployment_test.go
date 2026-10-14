package v2optical

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2qualification"
	"testing"
)

func TestMultiDeploymentBindsOperationAndProjectCapacity(t *testing.T) {
	for _, operation := range []string{"optical.panorama", "optical.focus_stack"} {
		pin, locks, _, _, params, originals := multiQualifiedFixture(t, operation)
		capacity, err := MultiResources(operation, params, originals)
		if err != nil {
			t.Fatal(err)
		}
		wire := multiDeploymentWire{SchemaVersion: 1, Projects: []multiDeploymentEntry{{Tenant: uuid.New(), Project: uuid.New(), Pin: pin, Locks: locks, MaxPixels: 200000000, Capacity: capacity}}}
		raw, err := canonical.Marshal(wire)
		if err != nil {
			t.Fatal(err)
		}
		deployment, err := DecodeMultiDeployment(operation, raw)
		if err != nil {
			t.Fatal(err)
		}
		for _, fault := range []string{"duplicate", "operation", "policy", "capacity", "unknown"} {
			var changed multiDeploymentWire
			if json.Unmarshal(raw, &changed) != nil {
				t.Fatal("fixture")
			}
			switch fault {
			case "duplicate":
				changed.Projects = append(changed.Projects, changed.Projects[0])
			case "operation":
				changed.Projects[0].Locks.Operation = "optical.perspective"
			case "policy":
				changed.Projects[0].Pin.RuntimeLockSHA = changed.Projects[0].Pin.DatasetSHA
			case "capacity":
				changed.Projects[0].Capacity.VRAMBytes = 1
			}
			body, err := canonical.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			if fault == "unknown" {
				var value map[string]any
				if json.Unmarshal(body, &value) != nil {
					t.Fatal("fixture")
				}
				value["enabled"] = true
				body, err = canonical.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := DecodeMultiDeployment(operation, body); err == nil {
				t.Fatal("invalid deployment accepted", fault)
			}
		}
		raw[0] = ' '
		if _, err := DecodeMultiDeployment(operation, []byte(deployment.raw)); err != nil {
			t.Fatal("deployment aliases input", err)
		}
	}
}
func TestMultiProjectSelectionRejectsForeignScopeBeforeIO(t *testing.T) {
	projects := &MultiProjects{services: map[[2]uuid.UUID]*MultiService{}}
	_, err := projects.SubmitProjectIntent(context.Background(), auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), uuid.NewString(), MultiIntent{})
	if err != v2qualification.ErrUnqualified {
		t.Fatal("unconfigured scope reached IO", err)
	}
}
