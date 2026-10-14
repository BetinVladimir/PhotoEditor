package delivery

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestFailureWitnessRequiresExactFrozenTerminalSlot(t *testing.T) {
	plan, lease, _ := expansionPlanFixture(t)
	page, err := plan.ExportChildren(lease.Run, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	child := page.Items[0]
	export, err := plan.NewChildExportID(lease.Run, child)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"failed", "cancelled", "pending", "ready", "foreign", "missing", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			observation := ExportObservation{ExportID: export, SnapshotDigest: child.SnapshotDigest, Binding: child.Binding, PresetSHA: child.Preset.PresetSHA, RunState: "failed", ExportState: "failed"}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "cancelled":
				observation.RunState = "cancelled"
			case "pending":
				observation.RunState = "running"
				observation.ExportState = "queued"
			case "ready":
				observation.RunState = "completed"
				observation.ExportState = "ready"
				observation.ArtifactReady = true
				observation.Artifact = &v2input.Ref{ArtifactID: uuid.New(), SHA256: digest([]byte("image"))}
			case "foreign":
				observation.ExportID = uuid.New()
			case "cancel":
				cancel()
			}
			observations := []ChildObservation{{SlotIndex: 0, Export: observation}}
			if scenario == "missing" {
				observations = nil
			}
			witness, err := plan.FindFailureWitness(ctx, lease.Run, observations)
			switch scenario {
			case "foreign", "missing", "cancel":
				if err == nil || witness != nil {
					t.Fatal(witness, err)
				}
			case "pending", "ready":
				if err != nil || witness != nil {
					t.Fatal(witness, err)
				}
			default:
				if err != nil || witness == nil || witness.SlotIndex != 0 || witness.ChildRun != child.RunID || witness.ExportID != export {
					t.Fatal(witness, err)
				}
			}
		})
	}
}
